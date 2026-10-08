package meters

import (
	"strings"
	"testing"
	"time"
)

func validMeter() MeterConfig {
	return MeterConfig{
		UnitIDs: []uint8{1},
		Source:  SourceConfig{Type: "tcp", UnitID: 1, TCP: TCPSourceConfig{Host: "smartfox.local"}},
		Map:     map[string]MappingConfig{"power_total": {Type: "register", Address: 41017, DType: "int32"}},
	}
}

func TestMeterDefaults(t *testing.T) {
	m := validMeter()
	m.Source.Type = "rtu"
	m.Source.RTU.Port = "/dev/ttyUSB0"
	m.ApplyDefaults()

	if m.Source.Timeout != 2*time.Second || m.Poll.Interval != time.Second || m.Poll.MaxBlockGap != 4 || m.Poll.MaxBlockSize != 125 {
		t.Errorf("defaults = %+v / %+v", m.Source, m.Poll)
	}
	if rtu := m.Source.RTU; rtu.BaudRate != 9600 || rtu.DataBits != 8 || rtu.Parity != "N" || rtu.StopBits != 1 {
		t.Errorf("RTU defaults = %+v, want 9600 8N1", rtu)
	}
	if mp := m.Map["power_total"]; mp.ByteOrder != "big" || mp.WordOrder != "big" {
		t.Errorf("mapping defaults = %+v, want big/big", mp)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate() = %v", err)
	}
}

func TestMeterValidate(t *testing.T) {
	for want, change := range map[string]func(*MeterConfig){
		"invalid source.unitID":     func(m *MeterConfig) { m.Source.UnitID = 0 },
		"invalid source.timeout":    func(m *MeterConfig) { m.Source.Timeout = -time.Second },
		"invalid poll.interval":     func(m *MeterConfig) { m.Poll.Interval = -time.Second },
		"invalid poll.maxBlockGap":  func(m *MeterConfig) { m.Poll.MaxBlockGap = -1 },
		"invalid poll.maxBlockSize": func(m *MeterConfig) { m.Poll.MaxBlockSize = 126 },
		"map requires":              func(m *MeterConfig) { m.Map = nil },
		"source.tcp.host":           func(m *MeterConfig) { m.Source.TCP.Host = "" },
		"invalid source.type":       func(m *MeterConfig) { m.Source.Type = "udp" },
		"invalid source.rtu":        func(m *MeterConfig) { m.Source.Type = "rtu" },
		"has invalid type":          func(m *MeterConfig) { m.Map["power_total"] = MappingConfig{Type: "magic"} },
		"has invalid dtype":         func(m *MeterConfig) { m.Map["power_total"] = MappingConfig{Type: "register", DType: "int8"} },
		"has invalid byteOrder": func(m *MeterConfig) {
			m.Map["power_total"] = MappingConfig{Type: "register", DType: "int32", ByteOrder: "middle"}
		},
		"requires value": func(m *MeterConfig) { m.Map["power_total"] = MappingConfig{Type: "fixed"} },
		"requires expr":  func(m *MeterConfig) { m.Map["power_total"] = MappingConfig{Type: "expr", Expr: " "} },
	} {
		m := validMeter()
		m.ApplyDefaults()
		change(&m)
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %v, want an error containing %q", err, want)
		}
	}
}

func TestSerialConfigValidate(t *testing.T) {
	valid := SerialConfig{Port: "/dev/ttyS0", BaudRate: 9600, DataBits: 8, Parity: "N", StopBits: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	for want, change := range map[string]func(*SerialConfig){
		"port is required": func(s *SerialConfig) { s.Port = "" },
		"invalid baudRate": func(s *SerialConfig) { s.BaudRate = 0 },
		"invalid dataBits": func(s *SerialConfig) { s.DataBits = 9 },
		"invalid parity":   func(s *SerialConfig) { s.Parity = "X" },
		"invalid stopBits": func(s *SerialConfig) { s.StopBits = 3 },
	} {
		s := valid
		change(&s)
		if err := s.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %v, want an error containing %q", err, want)
		}
	}
}

func TestCompileDeviceRejectsUnknownField(t *testing.T) {
	m := validMeter()
	m.Map["voltage_l1_n"] = MappingConfig{Type: "fixed", Value: 230} // removed alias
	if _, err := compileDevice(m); err == nil || !strings.Contains(err.Error(), "unsupported canonical field") {
		t.Errorf("compileDevice() = %v, want unsupported canonical field", err)
	}
}
