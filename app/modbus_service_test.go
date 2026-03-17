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
