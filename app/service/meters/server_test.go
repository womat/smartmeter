package meters

import (
	"net"
	"strconv"
	"testing"
	"time"

	mb "github.com/simonvetter/modbus"
	"github.com/womat/smartmeter/pkg/fronius"
)

// freePort returns a local TCP port that is free right now.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startTestServer(t *testing.T, port int) *ModbusServerService {
	t.Helper()
	s, err := NewModbusServerService(map[string]MeterConfig{"m": {UnitIDs: []uint8{1, 200}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(ListenConfig{TCP: ListenTCPConfig{Enabled: true, Host: "127.0.0.1", Port: port}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func dial(t *testing.T, port int, unitID uint8) *mb.ModbusClient {
	t.Helper()
	c, err := mb.NewClient(&mb.ClientConfiguration{URL: "tcp://127.0.0.1:" + strconv.Itoa(port), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Open(); err != nil {
		t.Fatal(err)
	}
	_ = c.SetUnitId(unitID)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestServerServesUnitIDsReadOnly(t *testing.T) {
	port := freePort(t)
	s := startTestServer(t, port)
	defer s.Close()

	for _, id := range []uint8{1, 200} {
		snapshot := fronius.NewSnapshot()
		snapshot.Set(fronius.FieldModbusAddr, float64(id))
		if err := s.WriteSnapshot(id, snapshot); err != nil {
			t.Fatal(err)
		}
	}

	for _, id := range []uint8{1, 200} {
		c := dial(t, port, id)
		if got, err := c.ReadRegister(40068, mb.HOLDING_REGISTER); err != nil || got != uint16(id) {
			t.Errorf("unit %d register 40068 = %d, %v; want %d", id, got, err, id)
		}
		if err := c.WriteRegister(4096, 1); err == nil {
			t.Errorf("unit %d: write single register succeeded, want IllegalFunction", id)
		}
	}
}

// TestServerCloseReleasesPortAndClients is what a configuration reload relies on: the next
// server binds the same port, and a connected client does not stay with the old one.
func TestServerCloseReleasesPortAndClients(t *testing.T) {
	port := freePort(t)
	old := startTestServer(t, port)
	c := dial(t, port, 1)
	if _, err := c.ReadRegister(4096, mb.HOLDING_REGISTER); err != nil {
		t.Fatal(err)
	}

	closed := make(chan struct{})
	go func() { _ = old.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocks while a client is connected")
	}
	if _, err := c.ReadRegister(4096, mb.HOLDING_REGISTER); err == nil {
		t.Error("the closed server still answers an existing connection")
	}

	next := startTestServer(t, port)
	defer next.Close()
	if _, err := dial(t, port, 1).ReadRegister(4096, mb.HOLDING_REGISTER); err != nil {
		t.Errorf("new server on the same port: %v", err)
	}
}

func TestSerialReady(t *testing.T) {
	since := time.Date(2026, 10, 8, 22, 0, 0, 0, time.UTC)
	if err := serialReady(nil); err != nil {
		t.Errorf("serialReady(nil) = %v, want nil without RTU listener", err)
	}
	if err := serialReady([]SerialStatus{{Port: "/dev/ttyS0", Connected: true, Since: since}}); err != nil {
		t.Errorf("serialReady(connected) = %v, want nil", err)
	}
	err := serialReady([]SerialStatus{{Port: "/dev/ttyUSB0", Error: "no such device", Since: since}})
	if err == nil || err.Error() != "serial port /dev/ttyUSB0 not available since 2026-10-08T22:00:00Z: no such device" {
		t.Errorf("serialReady(disconnected) = %v", err)
	}

	var none *ModbusServerService
	if none.Status() != nil || none.Ready() != nil {
		t.Error("a missing server must report no RTU port and be ready")
	}
}

func TestStartReportsMissingSerialPort(t *testing.T) {
	s, err := NewModbusServerService(map[string]MeterConfig{"m": {UnitIDs: []uint8{1}}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rtu := ListenRTUConfig{Enabled: true, SerialConfig: SerialConfig{Port: "/dev/does-not-exist", BaudRate: 9600, DataBits: 8, Parity: "N", StopBits: 1}}
	if err = s.Start(ListenConfig{RTU: rtu}); err == nil {
		t.Error("Start succeeded with a missing serial port")
	}
}
