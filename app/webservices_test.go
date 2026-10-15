package app

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLoadTLSCertFallback(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pem")

	if _, err := loadTLSCert(missing, missing, ProdEnv); err == nil {
		t.Error("env prod without certFile must fail instead of using the embedded certificate")
	}
	if _, err := loadTLSCert(missing, missing, DevEnv); err != nil {
		t.Errorf("env dev should fall back to the embedded certificate: %v", err)
	}
}

func TestServerErrorRestartsApp(t *testing.T) {
	app := New(NewConfig(), make(chan os.Signal), nil)
	app.HandleOSSignals()

	app.serverErr <- errors.New("listener died")

	select {
	case <-app.Restart():
	case <-time.After(5 * time.Second):
		t.Fatal("a web server error did not restart the App")
	}
	if app.ctx.Err() == nil {
		t.Error("the App context was not cancelled by the restart")
	}
}

func TestRejectedReloadKeepsApp(t *testing.T) {
	signals := make(chan os.Signal, 1)
	app := New(NewConfig(), signals, func() error { return errors.New("broken config") })
	app.HandleOSSignals()

	signals <- syscall.SIGHUP

	select {
	case <-app.Restart():
		t.Fatal("a rejected reload restarted the App")
	case <-time.After(200 * time.Millisecond):
	}
	if app.ctx.Err() != nil {
		t.Error("a rejected reload cancelled the App context")
	}
	app.cancelFunc()
}

// A client that offers at most TLS 1.2 is refused.
func TestTLSConfigRequires13(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pem")
	cert, err := loadTLSCert(missing, missing, DevEnv)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = newTLSConfig(cert)
	srv.StartTLS()
	defer srv.Close()

	for _, tt := range []struct {
		max  uint16
		fail bool
	}{
		{tls.VersionTLS12, true},
		{tls.VersionTLS13, false},
	} {
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // self-signed test certificate
			MaxVersion:         tt.max,
		}}}
		resp, err := client.Get(srv.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
		if (err != nil) != tt.fail {
			t.Errorf("max version %x: err = %v, want failure %v", tt.max, err, tt.fail)
		}
	}
}
