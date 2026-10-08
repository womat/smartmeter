// Package app provides the main application.
//
// It initializes S0 meters, handles MQTT publishing, periodic backups,
// web server startup, and OS signal handling for graceful shutdowns
// or restarts.
//
// Usage:
//
//	config := LoadConfig()
//	app := app.New(config, "/opt/smartmeteremu")
//	app.Run()
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
)

// VERSION holds the version information with the following logic in mind
//
//	4 ... fixed
//	0 ... year 2020, 1->year 2021, etc.
//	7 ... month of year (7=July)
//	the date format after the + is always the first of the month
//
// VERSION differs from semantic versioning as described in https://semver.org/
// but we keep the correct syntax.
// TODO: increase version number
const (
	VERSION = "1.6.2+20260228"
	MODULE  = "smartmeteremu"

	ModeStop    = 0
	ModeRestart = 1
)

// App is the main application struct.
// App is where the application is wired up.
type App struct {
	wg           sync.WaitGroup // wait group to track running webserver
	baseDir      string         // working directory
	config       *Config        // app configuration
	web          *http.Server   // HTTP server
	restart      chan struct{}  // signals application restart
	shutdown     chan struct{}  // signals application shutdown
	ctx          context.Context
	cancelFunc   context.CancelFunc
	modbusClient *ModbusService
	modbusServer *ModbusServerService

	// add your additional handler here
}

// New initializes the App struct but does not start services.
func New(config *Config, baseDir string) *App {
	ctx, cancel := context.WithCancel(context.Background())

	return &App{
		baseDir: baseDir,
		config:  config,
		web: &http.Server{
			Addr: net.JoinHostPort(config.Webserver.ListenHost, strconv.Itoa(config.Webserver.ListenPort)),
		},

		restart:    make(chan struct{}),
		shutdown:   make(chan struct{}),
		ctx:        ctx,
		cancelFunc: cancel,
	}
}

// Run initializes the application, starts services,
// and the web server, and sets up OS signal handling.
func (app *App) Run() (*App, error) {
	slog.Info("Initializing application")

	if err := app.Init(); err != nil {
		return app, err
	}

	// here start your services
	if len(app.config.Devices) > 0 {
		if err := app.modbusServer.Start(app.config.Listen); err != nil {
			slog.Error("Error starting modbus server", "error", err.Error())
			return app, err
		}
		if err := app.modbusClient.Start(app.ctx, app.modbusServer); err != nil {
			slog.Error("Error starting modbus polling service", "error", err.Error())
			return app, err
		}
		slog.Info("Modbus client and server started", "devices", len(app.config.Devices))
	}

	// handle the OS signals
	app.HandleOSSignals()

	slog.Info("Starting web server", "url", app.web.Addr)
	err := app.StartWebServer()
	if err != nil {
		slog.Error("Web server failed to start", "url", app.web.Addr, "error", err)
		return app, err
	}

	slog.Info("Module started successfully",
		"module", MODULE,
		"version", VERSION,
		"pid", os.Getpid(),
	)
	return app, nil
}

// Init prepares the application:
// - initialize serives
// - initializes API routes
func (app *App) Init() (err error) {

	if len(app.config.Devices) > 0 {
		app.modbusServer, err = NewModbusServerService(app.config)
		if err != nil {
			slog.Error("Failed to initialize Modbus server", "error", err)
			return err
		}

		app.modbusClient, err = NewModbusService(app.config)
		if err != nil {
			slog.Error("Failed to initialize Modbus client", "error", err)
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

// HandleOSSignals listens for SIGHUP, SIGTERM, and SIGINT signals.
func (app *App) HandleOSSignals() {

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
		defer signal.Stop(sig) // Cleanup: rollback signal.Notify

		slog.Debug("Starting signal handler")

		// Use select instead of a plain channel receive so the goroutine has
		// two exit paths and always terminates cleanly:
		//   - a signal is received and handled, or
		//   - the context is cancelled externally (e.g. from a concurrent shutdown).
		// Without this, the goroutine would block forever after signal.Reset()
		// on a SIGHUP restart, leaking one goroutine per reload cycle.
		select {
		case receivedSignal := <-sig:
			slog.Info("Received OS signal", "signal", receivedSignal)
			switch receivedSignal {
			case syscall.SIGHUP:
				slog.Info("SIGHUP received, initiating restart")
				app.shutdownProcedure(ModeRestart)
			case syscall.SIGTERM, syscall.SIGINT:
				slog.Info("SIGTERM/SIGINT received, stopping")
				app.shutdownProcedure(ModeStop)
			}
		case <-app.ctx.Done():
			// Context was cancelled externally – exit without triggering
			// a second shutdown procedure.
			slog.Debug("Signal handler: context cancelled, exiting goroutine")
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
	app.wg.Wait() //wait for the web server to shutdown before cleaning up resources

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
// Should be used to free up resources.
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
