package app

import (
	"math"
	"testing"
	"time"

	"github.com/womat/smartmeteremu/pkg/mbserver"
)

type fakeRegisterReader struct {
	registers map[uint16]uint16
}

func (r *fakeRegisterReader) ReadHoldingRegisters(address, quantity uint16) ([]uint16, error) {
	data := make([]uint16, quantity)
	for i := uint16(0); i < quantity; i++ {
		data[i] = r.registers[address+i]
	}
	return data, nil
}

func (r *fakeRegisterReader) Close() error { return nil }

func TestBuildReadBlocks(t *testing.T) {
	blocks := buildReadBlocks([]compiledMapping{
		{Config: MappingConfig{Address: 10}, Registers: 1},
		{Config: MappingConfig{Address: 12}, Registers: 2},
		{Config: MappingConfig{Address: 30}, Registers: 1},
	}, 1, 10)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Start != 10 || blocks[0].Quantity != 4 {
		t.Fatalf("expected first block [10,4], got [%d,%d]", blocks[0].Start, blocks[0].Quantity)
	}
	if blocks[1].Start != 30 || blocks[1].Quantity != 1 {
		t.Fatalf("expected second block [30,1], got [%d,%d]", blocks[1].Start, blocks[1].Quantity)
	}
}

func TestPollAndUpdateWritesFroniusRegisters(t *testing.T) {
	device, err := compileDevice(DeviceConfig{
		Name:   "main_meter",
		UnitID: 200,
		Source: SourceConfig{
			Type:    "tcp",
			UnitID:  1,
			Timeout: 2 * time.Second,
			TCP: TCPSourceConfig{
				Host: "127.0.0.1",
				Port: 502,
			},
		},
		Poll: PollConfig{
			Interval:     time.Second,
			MaxBlockGap:  4,
			MaxBlockSize: 125,
		},
		Map: map[string]MappingConfig{
			"device_id":     {Type: "fixed", Value: 285},
			"firmware":      {Type: "fixed", Value: 117},
			"serial_number": {Type: "fixed", Value: 99999999},
			"voltage_l1_n":  {Type: "register", Address: 52, DType: "uint16", ByteOrder: "big", WordOrder: "big", Scale: -1},
			"voltage_l2_n":  {Type: "register", Address: 54, DType: "uint16", ByteOrder: "big", WordOrder: "big", Scale: -1},
			"voltage_l3_n":  {Type: "register", Address: 56, DType: "uint16", ByteOrder: "big", WordOrder: "big", Scale: -1},
			"voltage_l1_l2": {Type: "expr", Expr: "${voltage_l1_n} * SQRT(3)"},
			"current_l1":    {Type: "register", Address: 58, DType: "uint32", ByteOrder: "big", WordOrder: "big", Scale: -3},
			"current_l2":    {Type: "register", Address: 62, DType: "uint32", ByteOrder: "big", WordOrder: "big", Scale: -3},
			"current_l3":    {Type: "register", Address: 66, DType: "uint32", ByteOrder: "big", WordOrder: "big", Scale: -3},
			"power_total":   {Type: "register", Address: 36, DType: "uint32", ByteOrder: "big", WordOrder: "big", Scale: 0},
			"power_l1":      {Type: "register", Address: 40, DType: "uint32", ByteOrder: "big", WordOrder: "big", Scale: 0},
			"power_l2":      {Type: "register", Address: 44, DType: "uint32", ByteOrder: "big", WordOrder: "big", Scale: 0},
			"power_l3":      {Type: "register", Address: 48, DType: "uint32", ByteOrder: "big", WordOrder: "big", Scale: 0},
			"energy_import": {Type: "register", Address: 0, DType: "uint64", ByteOrder: "big", WordOrder: "big", Scale: 0},
			"energy_export": {Type: "register", Address: 8, DType: "uint64", ByteOrder: "big", WordOrder: "big", Scale: 0},
			"frequency":     {Type: "register", Address: 76, DType: "uint16", ByteOrder: "big", WordOrder: "big", Scale: -2},
		},
	})
	if err != nil {
		t.Fatalf("compileDevice() error = %v", err)
	}

	device.Reader = &fakeRegisterReader{
		registers: map[uint16]uint16{
			0: 0, 1: 0, 2: 0, 3: 1000,
			8: 0, 9: 0, 10: 0, 11: 200,
			36: 0, 37: 500,
			40: 0, 41: 200,
			44: 0, 45: 150,
			48: 0, 49: 150,
			52: 2300,
			54: 2310,
			56: 2290,
			58: 0, 59: 10000,
			62: 0, 63: 11000,
			66: 0, 67: 12000,
			76: 5000,
		},
	}

	server := &ModbusServerService{server: mbserver.NewServer()}
	defer server.Close()
	if err := server.server.NewDevice(200); err != nil {
		t.Fatalf("NewDevice() error = %v", err)
	}

	if err := device.pollAndUpdate(server); err != nil {
		t.Fatalf("pollAndUpdate() error = %v", err)
	}

	tests := []struct {
		addr uint16
		want uint16
	}{
		{addr: 18, want: 285},
		{addr: 40068, want: 200},
		{addr: 40077, want: 2300},
		{addr: 40085, want: 5000},
		{addr: 40087, want: 500},
		{addr: 40115, want: 0},
		{addr: 40116, want: 1000},
	}

	for _, tc := range tests {
		got, err := server.server.GetHoldingRegister(200, tc.addr)
		if err != nil {
			t.Fatalf("GetHoldingRegister(%d) error = %v", tc.addr, err)
		}
		if got != tc.want {
			t.Fatalf("register %d = %d, want %d", tc.addr, got, tc.want)
		}
	}

	lineLine, err := server.server.GetHoldingRegister(200, 40081)
	if err != nil {
		t.Fatalf("GetHoldingRegister(40081) error = %v", err)
	}
	if want := uint16(math.Round(230 * math.Sqrt(3) * 10)); lineLine != want {
		t.Fatalf("register 40081 = %d, want %d", lineLine, want)
	}
}

// TestPollAndUpdateWritesLegacyFroniusMap checks the proprietary 4096 map against
// values read from the productive emulator on primary-meter (Smartfox as source).
func TestPollAndUpdateWritesLegacyFroniusMap(t *testing.T) {
	reg := func(address uint16, dtype string, scale int) MappingConfig {
		return MappingConfig{Type: "register", Address: address, DType: dtype, ByteOrder: "big", WordOrder: "big", Scale: scale}
	}

	device, err := compileDevice(DeviceConfig{
		Name:   "primary_meter",
		UnitID: 1,
		Source: SourceConfig{Type: "tcp", UnitID: 1, Timeout: time.Second, TCP: TCPSourceConfig{Host: "127.0.0.1", Port: 502}},
		Poll:   PollConfig{Interval: time.Second, MaxBlockGap: 4, MaxBlockSize: 125},
		Map: map[string]MappingConfig{
			"energy_import": reg(40999, "uint64", 0),
			"energy_export": reg(41003, "uint64", 0),
			"power_total":   reg(41017, "int32", 0),
			"power_l1":      reg(41019, "int32", 0),
			"power_l2":      reg(41021, "int32", 0),
			"power_l3":      reg(41023, "int32", 0),
			"voltage_l1":    reg(41025, "uint16", -1),
			"voltage_l2":    reg(41026, "uint16", -1),
			"voltage_l3":    reg(41027, "uint16", -1),
			"current_l1":    reg(41028, "uint32", -3),
			"current_l2":    reg(41030, "uint32", -3),
			"current_l3":    reg(41032, "uint32", -3),
			"pf_l1":         reg(41034, "int16", -4),
			"pf_l2":         reg(41035, "int16", -4),
			"pf_l3":         reg(41036, "int16", -4),
			"frequency":     reg(41037, "uint16", -2),
		},
	})
	if err != nil {
		t.Fatalf("compileDevice() error = %v", err)
	}

	neg := func(v int32) uint16 { return uint16(uint32(v) & 0xFFFF) }
	device.Reader = &fakeRegisterReader{
		registers: map[uint16]uint16{
			40999: 0, 41000: 0, 41001: 1014, 41002: 64250, // 66517754 Wh
			41003: 0, 41004: 0, 41005: 259, 41006: 4844, // 16978668 Wh
			41017: 0xFFFF, 41018: neg(-16),
			41019: 0, 41020: 39,
			41021: 0xFFFF, 41022: neg(-33),
			41023: 0xFFFF, 41024: neg(-22),
			41025: 2291, 41026: 2324, 41027: 2322,
			41028: 0, 41029: 571,
			41030: 0, 41031: 413,
			41032: 0, 41033: 571,
			41034: 8200, 41035: neg(-9200), 41036: neg(-9900),
			41037: 5000,
		},
	}

	server := &ModbusServerService{server: mbserver.NewServer()}
	defer server.Close()

	if err := device.pollAndUpdate(server); err != nil {
		t.Fatalf("pollAndUpdate() error = %v", err)
	}

	get := func(addr uint16) uint16 {
		t.Helper()
		v, err := server.server.GetHoldingRegister(1, addr)
		if err != nil {
			t.Fatalf("GetHoldingRegister(%d) error = %v", addr, err)
		}
		return v
	}
	get32 := func(addr uint16) uint32 { return uint32(get(addr))<<16 | uint32(get(addr+1)) }

	// Reference values read from the productive emulator (old code, emu.yaml).
	tests32 := []struct {
		addr uint16
		want int64
	}{
		{4096, 229100}, {4098, 232400}, {4100, 232200}, // mV
		{4102, 571}, {4104, 413}, {4106, 571}, // mA
		{4116, -1600},                      // W*100, signed
		{4124, 66517754}, {4128, 16978668}, // Wh
		{4140, 3900}, {4142, -3300}, {4144, -2200}, // W*100, signed
	}
	for _, tc := range tests32 {
		got := int64(int32(get32(tc.addr)))
		if got != tc.want {
			t.Errorf("register %d = %d, want %d", tc.addr, got, tc.want)
		}
	}

	if want := uint32(math.Round(229.1 * math.Sqrt(3) * 1000)); get32(4110) != want {
		t.Errorf("register 4110 = %d, want %d", get32(4110), want)
	}

	tests16 := []struct {
		addr uint16
		want int16
	}{
		{4134, 500},                          // Hz*10
		{4164, 82}, {4165, -92}, {4166, -99}, // PF*100
		{18, 285}, {768, 117}, // identification
	}
	for _, tc := range tests16 {
		if got := int16(get(tc.addr)); got != tc.want {
			t.Errorf("register %d = %d, want %d", tc.addr, got, tc.want)
		}
	}

	// SunSpec block: PF in percent with PF_SF -1 and end block after the meter model.
	if got := int16(get(40103)); got != 820 {
		t.Errorf("register 40103 (PFphA) = %d, want 820", got)
	}
	if got := int16(get(40106)); got != -1 {
		t.Errorf("register 40106 (PF_SF) = %d, want -1", got)
	}
	if got := get(40176); got != 0xFFFF {
		t.Errorf("register 40176 (end block ID) = %#x, want 0xffff", got)
	}
	if got := get(40177); got != 0 {
		t.Errorf("register 40177 (end block L) = %d, want 0", got)
	}
}
