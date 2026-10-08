package meters

import (
	"errors"
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

// startTestServer starts a server for unit IDs 1 and 200 on port, both online as after the
// first valid snapshot.
func startTestServer(t *testing.T, port int) *ModbusServerService {
	t.Helper()
	s := startSilentTestServer(t, port)
	for _, id := range []uint8{1, 200} {
		if err := s.SetOnline(id, true); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// startSilentTestServer starts a server for unit IDs 1 and 200 as NewModbusServerService leaves
// it: both silent until the first valid snapshot.
func startSilentTestServer(t *testing.T, port int) *ModbusServerService {
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

// TestServerSilentUntilOnline: before the first valid snapshot a client gets exception 11
// instead of empty registers, which an inverter would take as 0 W.
func TestServerSilentUntilOnline(t *testing.T) {
	port := freePort(t)
	s := startSilentTestServer(t, port)
	defer s.Close()

	c := dial(t, port, 1)
	if _, err := c.ReadRegister(4096, mb.HOLDING_REGISTER); err == nil {
		t.Fatal("unit 1 answers before the first snapshot")
	}
	if err := s.SetOnline(1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadRegister(4096, mb.HOLDING_REGISTER); err != nil {
		t.Errorf("unit 1 online: %v", err)
	}
}

func TestActivity(t *testing.T) {
	port := freePort(t)
	s := startTestServer(t, port)
	defer s.Close()

	c := dial(t, port, 1)
	for range 3 {
		if _, err := c.ReadRegister(4096, mb.HOLDING_REGISTER); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.Activity().TCP.Requests < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	a := s.Activity()
	if a.TCP == nil || a.TCP.Requests != 3 || a.TCP.Clients != 1 || a.TCP.Address != "127.0.0.1:"+strconv.Itoa(port) {
		t.Errorf("Activity().TCP = %+v, want 3 requests, 1 client on port %d", a.TCP, port)
	}
	if a.RTU != nil {
		t.Errorf("Activity().RTU = %+v, want nil without RTU listener", a.RTU)
	}
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

func TestRegisters(t *testing.T) {
	port := freePort(t)
	s := startTestServer(t, port)
	defer s.Close()

	snapshot := fronius.NewSnapshot()
	snapshot.Set(fronius.FieldPowerTotal, -1234)
	snapshot.Set(fronius.FieldVoltageL1, 231.6)
	for _, id := range []uint8{1, 200} {
		snapshot.Set(fronius.FieldModbusAddr, float64(id))
		if err := s.WriteSnapshot(id, snapshot); err != nil {
			t.Fatal(err)
		}
	}

	one, err := s.Registers(1)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Registers(200)
	if err != nil {
		t.Fatal(err)
	}
	if !one.Online || len(one.Proprietary) != len(fronius.ProprietaryRegisterMap) || len(one.SunSpec) != len(fronius.SunSpecRegisterMap) {
		t.Fatalf("Registers(1) = online %v, %d/%d rows", one.Online, len(one.Proprietary), len(one.SunSpec))
	}

	row := func(rows []RegisterRow, addr int) RegisterRow {
		for _, r := range rows {
			if r.Addr == addr {
				return r
			}
		}
		t.Fatalf("no row for %d", addr)
		return RegisterRow{}
	}
	if r := row(one.Proprietary, 4116); r.Field != "power_total" || r.Type != "int32" || r.Value != -1234 || len(r.Raw) != 2 {
		t.Errorf("row 4116 = %+v, want power_total int32 -1234", r)
	}
	if r := row(one.Proprietary, 4096); r.Value != 231.6 {
		t.Errorf("row 4096 = %+v, want 231.6", r)
	}
	if r := row(one.SunSpec, 40004); r.Field != "" || r.Text != "Fronius" {
		t.Errorf("row 40004 = %+v, want the fixed text Fronius", r)
	}

	// Same snapshot on both unit IDs; only the SunSpec Modbus address differs.
	for i := range one.SunSpec {
		a, b := one.SunSpec[i], other.SunSpec[i]
		if a.Addr == 40068 {
			if a.Value != 1 || b.Value != 200 {
				t.Errorf("40068 = %g / %g, want 1 / 200", a.Value, b.Value)
			}
			continue
		}
		if a.Value != b.Value || a.Text != b.Text {
			t.Errorf("%d differs between unit 1 (%g) and 200 (%g)", a.Addr, a.Value, b.Value)
		}
	}

	if _, err = s.Registers(9); !errors.Is(err, ErrUnknownUnit) {
		t.Errorf("Registers(9) error = %v, want ErrUnknownUnit", err)
	}
}
