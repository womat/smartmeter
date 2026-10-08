// Package app provides the main application wiring for smartmeter.
//
// It polls the upstream meters, serves them as Fronius Smart Meters over Modbus TCP/RTU,
// runs the HTTPS API and handles the OS signals for graceful stops and configuration
// reloads. One App lives for one configuration; cmd/main.go builds a new one on every reload.
//
// Usage:
//
//	cfg, err := app.LoadConfig(file)          // then cfg.Validate()
//	signals := make(chan os.Signal, 1)
//	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
//	a, err := app.New(cfg, signals, checkReload).Run()
//	select {
//	case <-a.Restart():  // build the next App
//	case <-a.Shutdown(): // exit
//	}
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"syscall"

	"github.com/womat/smartmeter/app/service/meters"
)

// VERSION is the application version, following semantic versioning
// as described in https://semver.org/.
//
// It is not maintained in source: the Git tag is the single source of truth and
// the value is injected at build time via -ldflags (see Makefile and
// .goreleaser.yaml). The "dev" default applies to builds made without them.
var VERSION = "dev"

const (
	MODULE = "smartmeter"

	ModeStop    = 0
	ModeRestart = 1
)

// App is the main application struct.
// App is where the application is wired up.
type App struct {
	wg           sync.WaitGroup // tracks the web server goroutine
	config       *Config        // app configuration
	web          *http.Server   // HTTP server
	modbusClient *meters.ModbusService
	modbusServer *meters.ModbusServerService
	signals      <-chan os.Signal // OS signals, subscribed once by the caller for all lifecycles
	checkReload  func() error     // loads and validates the config file before a SIGHUP restart
	serverErr    chan error       // reports a web server that stopped on its own
	restart      chan struct{}    // signals application restart
	shutdown     chan struct{}    // signals application shutdown
	ctx          context.Context
	cancelFunc   context.CancelFunc
}

// New initializes the App struct but does not start services.
//
// signals must already be subscribed (signal.Notify) to SIGHUP, SIGTERM and SIGINT, and stay
// subscribed across restarts: a signal arriving while one App is torn down and the next is
// built then waits in the channel for the next App, instead of hitting the default action.
//
// checkReload is called on SIGHUP before anything is torn down. If it reports an error, the
// restart is refused and the App keeps running with its current configuration, so a broken
// config file cannot stop the inverter from getting its values. Passing nil skips the check.
func New(config *Config, signals <-chan os.Signal, checkReload func() error) *App {
	ctx, cancel := context.WithCancel(context.Background())

	return &App{
		config:      config,
		signals:     signals,
		checkReload: checkReload,
		web: &http.Server{
			Addr: net.JoinHostPort(config.Webserver.ListenHost, strconv.Itoa(config.Webserver.ListenPort)),
		},

		serverErr:  make(chan error, 1),
		restart:    make(chan struct{}),
		shutdown:   make(chan struct{}),
		ctx:        ctx,
		cancelFunc: cancel,
	}
}

// Run initializes the application, starts the Modbus server, the polling of the upstream
// meters and the web server, and sets up OS signal handling.
func (app *App) Run() (*App, error) {
	slog.Debug("Initializing application")

	if err := app.Init(); err != nil {
		return app, app.abort(err)
	}

	if len(app.config.Meter) > 0 {
		if err := app.modbusServer.Start(app.config.Listen); err != nil {
			slog.Error("Failed to start Modbus server", "error", err)
			return app, app.abort(err)
		}
		if err := app.modbusClient.Start(app.ctx, app.modbusServer); err != nil {
			slog.Error("Failed to start polling the meters", "error", err)
			return app, app.abort(err)
		}
		slog.Info("Modbus server and meter polling started", "meters", meters.Names(app.config.Meter))
	}

	// handle the OS signals
	app.HandleOSSignals()

	slog.Info("Starting web server", "url", app.web.Addr)
	err := app.StartWebServer()
	if err != nil {
		slog.Error("Web server failed to start", "url", app.web.Addr, "error", err)
		return app, app.abort(err)
	}

	slog.Info("Module started successfully",
		"module", MODULE,
		"version", VERSION,
		"pid", os.Getpid(),
	)
	return app, nil
}

// Init prepares the application:
// - creates the Modbus server with one device per unit ID
// - compiles the polling of every meter
// - initializes API routes
func (app *App) Init() (err error) {

	if len(app.config.Meter) > 0 {
		app.modbusServer, err = meters.NewModbusServerService(app.config.Meter)
		if err != nil {
			slog.Error("Failed to initialize Modbus server", "error", err)
			return err
		}

		app.modbusClient, err = meters.NewModbusService(app.config.Meter)
		if err != nil {
			slog.Error("Failed to initialize meter polling", "error", err)
			return err
		}
	}

	// initRoutes should always be called at the end
	slog.Debug("Initializing API routes")
	app.SetupRoutes()

	return nil
}

// Restart returns a read-only channel for restart signals.
func (app *App) Restart() <-chan struct{} {
	return app.restart
}

// Shutdown returns a read-only channel for shutdown signals.
func (app *App) Shutdown() <-chan struct{} {
	return app.shutdown
}

// HandleOSSignals handles SIGHUP (restart), SIGTERM and SIGINT (stop) from app.signals, and
// restarts the App when the web server stopped on its own.
//
// The subscription itself belongs to the caller and outlives this App, so nothing here
// stops or resets it; one goroutine per App consumes at most one signal. Being the only
// caller of shutdownProcedure, it also rules out two shutdowns running at once.
func (app *App) HandleOSSignals() {

	go func() {
		slog.Debug("Starting signal handler")

		// Use select instead of a plain channel receive so the goroutine has
		// two exit paths and always terminates cleanly:
		//   - a signal or a server error is received and handled, or
		//   - the context is cancelled externally (e.g. from a concurrent shutdown).
		// Without the second path the goroutine would outlive its App and take
		// the next signal away from the App that replaced it. The loop only
		// continues after a SIGHUP whose config was rejected.
		for {
			select {
			case receivedSignal := <-app.signals:
				slog.Info("Received OS signal", "signal", receivedSignal)
				switch receivedSignal {
				case syscall.SIGHUP:
					if app.checkReload != nil {
						if err := app.checkReload(); err != nil {
							slog.Error("Config reload rejected, keeping the running configuration", "error", err)
							continue
						}
					}
					slog.Info("SIGHUP received, initiating restart")
					app.shutdownProcedure(ModeRestart)
				case syscall.SIGTERM, syscall.SIGINT:
					slog.Info("SIGTERM/SIGINT received, stopping")
					app.shutdownProcedure(ModeStop)
				}
				return
			case err := <-app.serverErr:
				slog.Error("Web server stopped unexpectedly, initiating restart", "error", err)
				app.shutdownProcedure(ModeRestart)
				return
			case <-app.ctx.Done():
				// Context was cancelled externally – exit without triggering
				// a second shutdown procedure.
				slog.Debug("Signal handler: context cancelled, exiting goroutine")
				return
			}
		}
	}()
}

// shutdownProcedure gracefully stops or restarts the app based on mode.
//   - ModeStop: graceful shutdown the web server, Cleanup app resources and exit the application.
//   - ModeRestart: graceful shutdown the web server and Cleanup app resources and restart the application.
func (app *App) shutdownProcedure(mode int) {
	slog.Info("Initiating shutdown", "mode", mode)

	// cancel the application context to stop all running goroutines
	app.cancelFunc()
	app.wg.Wait() // wait for the web server to shut down before cleaning up resources

	if err := app.Cleanup(); err != nil {
		slog.Error("Cleanup failed", "error", err)
	}

	switch mode {
	case ModeRestart:
		slog.Info("Shutdown complete, restarting")
		app.restart <- struct{}{}
		// Channels are intentionally left open: cmd/main.go receives the restart
		// signal and calls New(), which creates fresh channels for the next lifecycle.
	case ModeStop:
		slog.Info("Module stopped", "module", MODULE, "version", VERSION, "pid", os.Getpid())
		app.shutdown <- struct{}{}
		close(app.shutdown)
	}

}

// Cleanup releases application resources.
// It's called when the application is shutdown or restarted.
// Polling stops first, so no snapshot is written to a server that is already closed;
// closing the server releases the TCP port and the serial line for the next App.
func (app *App) Cleanup() error {
	var errs error

	if app.modbusClient != nil {
		errs = errors.Join(errs, app.modbusClient.Close())
	}
	if app.modbusServer != nil {
		errs = errors.Join(errs, app.modbusServer.Close())
	}

	return errs
}

// abort undoes a Run that failed half way: it stops the goroutines already started and
// releases what Init and Run acquired, so the caller can start another App with the same
// port and serial line. It returns err.
func (app *App) abort(err error) error {
	app.cancelFunc()
	app.wg.Wait()
	if cerr := app.Cleanup(); cerr != nil {
		slog.Error("Cleanup failed", "error", cerr)
	}
	return err
}
