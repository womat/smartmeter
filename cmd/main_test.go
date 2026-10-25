package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/womat/golib/v2/web"
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

// writeConfig writes a config without meters, serving only the web server on webPort with the
// embedded dev certificate, and a token file next to it. extra is appended to the file.
func writeConfig(t *testing.T, file string, webPort int, extra string) {
	t.Helper()
	dir := filepath.Dir(file)
	if err := os.WriteFile(filepath.Join(dir, "token-hashes"), []byte(fmt.Sprintf("test %x\n", web.HashToken("test-key"))), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`env: dev
webserver:
  listenHost: 127.0.0.1
  listenPort: %d
  tokenFile: token-hashes
  certFile: %s
%s`, webPort, filepath.Join(dir, "missing-cert.pem"), extra)
	if err := os.WriteFile(file, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

// answers reports whether the web server on port answers /version.
func answers(port int) bool {
	client := &http.Client{Timeout: 200 * time.Millisecond, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get("https://127.0.0.1:" + strconv.Itoa(port) + "/version")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRunRefusesBrokenConfig: a config that does not load ends with exitConfig, so systemd does
// not restart it (RestartPreventExitStatus=78).
func TestRunRefusesBrokenConfig(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, file, freePort(t), "unknownKey: 1\n")
	if code := run(file, false); code != exitConfig {
		t.Errorf("run() = %d, want %d", code, exitConfig)
	}
}

// TestRunRefusesUnwritableLogDestination: a log file that cannot be opened is a config error too.
func TestRunRefusesUnwritableLogDestination(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeConfig(t, file, freePort(t), "logDestination: "+filepath.Join(dir, "missing", "smartmeter.log")+"\n")
	if code := run(file, false); code != exitConfig {
		t.Errorf("run() = %d, want %d", code, exitConfig)
	}
}

// TestRunFailingFirstStart: without a configuration that ever ran there is nothing to fall
// back to, and a start that fails (here the web port is busy) ends with 1.
func TestRunFailingFirstStart(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	file := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, file, busy.Addr().(*net.TCPAddr).Port, "")
	if code := run(file, false); code != 1 {
		t.Errorf("run() = %d, want 1", code)
	}
}

// TestRunFallsBackToLastGoodConfig: a reload whose config validates but fails to start (the new
// web port is busy) continues with the previous configuration; SIGTERM then ends with 0.
func TestRunFallsBackToLastGoodConfig(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	file := filepath.Join(t.TempDir(), "config.yaml")
	goodPort := freePort(t)
	writeConfig(t, file, goodPort, "")

	result := make(chan int, 1)
	go func() { result <- run(file, false) }()
	waitFor(t, "the first start", func() bool { return answers(goodPort) })

	writeConfig(t, file, busy.Addr().(*net.TCPAddr).Port, "")
	if err = syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the restart to stop the first App", func() bool { return !answers(goodPort) })
	waitFor(t, "the fallback to the previous config", func() bool { return answers(goodPort) })

	if err = syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-result:
		if code != 0 {
			t.Errorf("run() = %d, want 0", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after SIGTERM")
	}
}
