package meters

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/womat/mbserver"
)

// startFakeSmartfox serves the given registers on unit ID 1 over Modbus TCP, as upstream meter.
func startFakeSmartfox(t *testing.T, registers map[uint16]uint16) (port int, server *mbserver.Server) {
	t.Helper()
	port = freePort(t)
	return port, startFakeSmartfoxOn(t, port, registers)
}

// startFakeSmartfoxOn is startFakeSmartfox on a given port, e.g. to restart the source.
func startFakeSmartfoxOn(t *testing.T, port int, registers map[uint16]uint16) (server *mbserver.Server) {
	t.Helper()
	server = mbserver.NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); server.Close() })
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := server.ListenTCP(ctx, "127.0.0.1:"+strconv.Itoa(port)); err != nil {
		t.Fatal(err)
	}
	for addr, value := range registers {
		if err := server.SetHoldingRegisters(1, addr, []uint16{value}); err != nil {
			t.Fatal(err)
		}
	}
	return server
}

func smartfoxMeter(port int) MeterConfig {
	reg := func(address uint16, dtype string, scale int) MappingConfig {
		return MappingConfig{Type: "register", Address: address, DType: dtype, ByteOrder: "big", WordOrder: "big", Scale: scale}
	}
	m := MeterConfig{
		Name:       "primary_meter",
		UnitIDs:    []uint8{1, 200},
		MaxCurrent: 63,
		Source:     SourceConfig{Type: "tcp", UnitID: 1, Timeout: time.Second, TCP: TCPSourceConfig{Host: "127.0.0.1", Port: port}},
		Poll:       PollConfig{Interval: 50 * time.Millisecond, MaxBlockGap: 10, MaxBlockSize: 125},
		Map: map[string]MappingConfig{
			"energy_import": reg(40999, "uint64", 0),
			"power_total":   reg(41017, "int32", 0),
			"voltage_l1":    reg(41025, "uint16", -1),
			"frequency":     reg(41037, "uint16", -2),
		},
	}
	return m
}

// TestPollingEndToEnd polls a Modbus TCP upstream meter and serves the values on both unit
// IDs, as in production; a change upstream shows up within a few poll intervals.
func TestPollingEndToEnd(t *testing.T) {
	upstreamPort, upstream := startFakeSmartfox(t, map[uint16]uint16{
		41001: 1014, 41002: 64250, // 66517754 Wh
		41017: 0xFFFF, 41018: 0xFFF0, // -16 W
		41025: 2291,
		41037: 5000,
	})
	meters := map[string]MeterConfig{"primary_meter": smartfoxMeter(upstreamPort)}

	server, err := NewModbusServerService(meters)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := NewModbusService(meters)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Start(context.Background(), server); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	waitUntil := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitUntil("the first snapshot", func() bool { return client.Ready(time.Now()) == nil })

	read32 := func(unit uint8, addr uint16) uint32 {
		regs, err := server.server.HoldingRegisters(unit, addr, 2)
		if err != nil {
			t.Fatal(err)
		}
		return uint32(regs[0])<<16 | uint32(regs[1])
	}
	for _, unit := range []uint8{1, 200} {
		if got := int32(read32(unit, 4116)); got != -1600 {
			t.Errorf("unit %d register 4116 = %d, want -1600", unit, got)
		}
		if got := read32(unit, 4124); got != 66517754 {
			t.Errorf("unit %d register 4124 = %d, want 66517754", unit, got)
		}
		if got, _ := server.holdingRegister(unit, 40085); got != 5000 {
			t.Errorf("unit %d register 40085 = %d, want 5000", unit, got)
		}
	}

	if err = upstream.SetHoldingRegisters(1, 41025, []uint16{2350}); err != nil {
		t.Fatal(err)
	}
	waitUntil("the new voltage", func() bool { return read32(1, 4096) == 235000 })
}

// TestPollingReconnectsAfterSourceDropsConnection: the source closes the connection (on
// primary-meter the Smartfox did, "write: broken pipe" for hours) and is back a moment later.
// smartmeter must reconnect on its own and serve the new values, without a restart.
func TestPollingReconnectsAfterSourceDropsConnection(t *testing.T) {
	registers := map[uint16]uint16{41017: 0, 41018: 100, 41025: 2300, 41037: 5000}
	port, upstream := startFakeSmartfox(t, registers)
	meters := map[string]MeterConfig{"primary_meter": smartfoxMeter(port)}

	server, err := NewModbusServerService(meters)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := NewModbusService(meters)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Start(context.Background(), server); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	power := func() int32 {
		regs, err := server.server.HoldingRegisters(1, 4116, 2)
		if err != nil {
			t.Fatal(err)
		}
		return int32(uint32(regs[0])<<16 | uint32(regs[1]))
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitFor("the first snapshot", func() bool { return power() == 10000 })

	upstream.Close() // drops the connection smartmeter holds
	waitFor("a failed poll", func() bool { return client.Status(time.Now())["primary_meter"].LastError != "" })

	registers[41018] = 250
	startFakeSmartfoxOn(t, port, registers)
	waitFor("the values of the restarted source", func() bool { return power() == 25000 })
}

func TestPollReporterThrottles(t *testing.T) {
	var buf strings.Builder
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(defaultLogger)

	d := &compiledDevice{Config: MeterConfig{Name: "m", Poll: PollConfig{Interval: time.Second}}}
	report := d.pollReporter()
	for range 125 { // two minutes and five seconds without the source
		report(errors.New("broken pipe"))
	}
	report(nil)

	if got := strings.Count(buf.String(), "Modbus poll failed"); got != 3 {
		t.Errorf("logged %d failures in 125 polls, want 3 (the first, after 60 and 120)", got)
	}
	if !strings.Contains(buf.String(), "Source answers again") || !strings.Contains(buf.String(), "failedPolls=125") {
		t.Errorf("log = %s, want the recovery with failedPolls=125", buf.String())
	}
}

func TestStartFailsWhenUpstreamIsUnreachable(t *testing.T) {
	meters := map[string]MeterConfig{"primary_meter": smartfoxMeter(freePort(t))}
	client, err := NewModbusService(meters)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Start(context.Background(), newTestServer()); err == nil {
		_ = client.Close()
		t.Fatal("Start succeeded without an upstream meter")
	}
}

func TestParseModbusParity(t *testing.T) {
	for _, value := range []string{"N", "e", "O"} {
		if _, err := parseModbusParity(value); err != nil {
			t.Errorf("parseModbusParity(%q) = %v", value, err)
		}
	}
	if _, err := parseModbusParity("M"); err == nil {
		t.Error("parseModbusParity(M) succeeded")
	}
}
