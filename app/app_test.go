package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/womat/golib/web"
	"github.com/womat/mbserver"
	"github.com/womat/smartmeter/app/service/health"
	"github.com/womat/smartmeter/app/service/meters"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// testAppConfig returns a config with one meter polling a fake upstream meter, the Modbus
// server on modbusPort and the web server on webPort.
func testAppConfig(t *testing.T, modbusPort, webPort int) *Config {
	t.Helper()

	upstream := mbserver.NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); upstream.Close() })
	upstreamPort := freePort(t)
	if err := upstream.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := upstream.ListenTCP(ctx, "127.0.0.1:"+strconv.Itoa(upstreamPort)); err != nil {
		t.Fatal(err)
	}

	cfg := NewConfig()
	cfg.Webserver.TokenFile = "/opt/smartmeter/etc/tokens"
	cfg.Webserver.Tokens = []web.Token{{Name: "test", Hash: web.HashToken("test-key")}}
	cfg.Webserver.ListenHost = "127.0.0.1"
	cfg.Webserver.ListenPort = webPort
	cfg.Webserver.CertFile = "/nonexistent/cert.pem" // embedded dev certificate (env dev)
	cfg.Listen.TCP = &meters.ListenTCPConfig{Host: "127.0.0.1", Port: modbusPort}
	cfg.Meter = map[string]meters.MeterConfig{"m": {
		UnitIds: []uint8{1},
		Source:  meters.SourceConfig{Type: "tcp", UnitId: 1, TCP: meters.TCPSourceConfig{Host: "127.0.0.1", Port: upstreamPort}},
		Poll:    meters.PollConfig{Interval: 50 * time.Millisecond},
		Map:     map[string]meters.MappingConfig{"frequency": {Type: "register", Address: 0, DType: "uint16", Scale: -2}},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// waitReady waits until /health reports every meter with current values.
func waitReady(t *testing.T, webPort int) {
	t.Helper()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1:"+strconv.Itoa(webPort)+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-key")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if resp, err := client.Do(req); err == nil {
			var h health.Model
			ok := resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&h) == nil && len(h.Meters) > 0
			_ = resp.Body.Close()
			for _, st := range h.Meters {
				ok = ok && st.Ready
			}
			if ok {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("/health did not report every meter ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRunReloadAndStop runs the App as cmd/main.go does: start, SIGHUP restart with the same
// ports, SIGTERM stop.
func TestRunReloadAndStop(t *testing.T) {
	modbusPort, webPort := freePort(t), freePort(t)
	cfg := testAppConfig(t, modbusPort, webPort)
	signals := make(chan os.Signal, 1)

	a, err := New(cfg, signals, nil).Run()
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, webPort)

	signals <- syscall.SIGHUP
	select {
	case <-a.Restart():
	case <-time.After(5 * time.Second):
		t.Fatal("SIGHUP did not restart the App")
	}

	// The next App binds the same Modbus and web ports.
	a, err = New(cfg, signals, nil).Run()
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	waitReady(t, webPort)

	signals <- syscall.SIGTERM
	select {
	case <-a.Shutdown():
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM did not stop the App")
	}
}

// TestFailedRunReleasesModbusPort: a start that fails late (web port busy) must not keep the
// Modbus port, or the fallback to the previous configuration could not bind it.
func TestFailedRunReleasesModbusPort(t *testing.T) {
	modbusPort, webPort := freePort(t), freePort(t)
	busy, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(webPort))
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	if _, err = New(testAppConfig(t, modbusPort, webPort), make(chan os.Signal, 1), nil).Run(); err == nil {
		t.Fatal("Run succeeded with the web port in use")
	}

	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(modbusPort))
	if err != nil {
		t.Fatalf("Modbus port still in use after the failed start: %v", err)
	}
	_ = l.Close()
}
