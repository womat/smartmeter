package fronius

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// CanonicalField is the unique name of a measurement in the internal snapshot.
type CanonicalField string

const (
	FieldDeviceID     CanonicalField = "device_id"
	FieldFirmware     CanonicalField = "firmware"
	FieldSerialNumber CanonicalField = "serial_number"
	FieldSerialStr    CanonicalField = "serial_str"

	FieldSunSpecMagic CanonicalField = "sunspec_magic"
	FieldModbusAddr   CanonicalField = "modbus_addr"

	FieldPowerTotal   CanonicalField = "power_total"
	FieldPowerL1      CanonicalField = "power_l1"
	FieldPowerL2      CanonicalField = "power_l2"
	FieldPowerL3      CanonicalField = "power_l3"
	FieldVoltageL1    CanonicalField = "voltage_l1"
	FieldVoltageL2    CanonicalField = "voltage_l2"
	FieldVoltageL3    CanonicalField = "voltage_l3"
	FieldCurrentL1    CanonicalField = "current_l1"
	FieldCurrentL2    CanonicalField = "current_l2"
	FieldCurrentL3    CanonicalField = "current_l3"
	FieldEnergyImport CanonicalField = "energy_import"
	FieldEnergyExport CanonicalField = "energy_export"
	FieldFrequency    CanonicalField = "frequency"
	FieldPFL1         CanonicalField = "pf_l1"
	FieldPFL2         CanonicalField = "pf_l2"
	FieldPFL3         CanonicalField = "pf_l3"

	FieldCurrentTotal  CanonicalField = "current_total"
	FieldVoltageAvgPN  CanonicalField = "voltage_avg_pn"
	FieldVoltageAvgPP  CanonicalField = "voltage_avg_pp"
	FieldVoltageL1L2   CanonicalField = "voltage_l1_l2"
	FieldVoltageL2L3   CanonicalField = "voltage_l2_l3"
	FieldVoltageL3L1   CanonicalField = "voltage_l3_l1"
	FieldApparentTotal CanonicalField = "apparent_total"
	FieldApparentL1    CanonicalField = "apparent_l1"
	FieldApparentL2    CanonicalField = "apparent_l2"
	FieldApparentL3    CanonicalField = "apparent_l3"
	FieldReactiveTotal CanonicalField = "reactive_total"
	FieldReactiveL1    CanonicalField = "reactive_l1"
	FieldReactiveL2    CanonicalField = "reactive_l2"
	FieldReactiveL3    CanonicalField = "reactive_l3"
	FieldPFTotal       CanonicalField = "pf_total"
	FieldEnergyExpL1   CanonicalField = "energy_export_l1"
	FieldEnergyExpL2   CanonicalField = "energy_export_l2"
	FieldEnergyExpL3   CanonicalField = "energy_export_l3"
	FieldEnergyImpL1   CanonicalField = "energy_import_l1"
	FieldEnergyImpL2   CanonicalField = "energy_import_l2"
	FieldEnergyImpL3   CanonicalField = "energy_import_l3"
)

type RegType uint8

const (
	RegUint16 RegType = iota
	RegUint32
	RegInt16
	RegInt32
	RegFloat32
	RegString
)

type RegisterEntry struct {
	Addr      int
	Field     CanonicalField
	Type      RegType
	SF        int
	NRegs     int
	FixValue  float64
	FixString string
}

func fix(addr int, t RegType, val any, nRegs ...int) RegisterEntry {
	e := RegisterEntry{Addr: addr, Type: t}
	if t == RegString {
		e.FixString, _ = val.(string)
		if len(nRegs) > 0 {
			e.NRegs = nRegs[0]
		}
		return e
	}

	switch v := val.(type) {
	case float64:
		e.FixValue = v
	case int:
		e.FixValue = float64(v)
	}
	return e
}

// FroniusRegisterMap is the complete map served for every unit ID.
// Addresses are 0-based register numbers as sent on the wire.
var FroniusRegisterMap = slices.Concat(ProprietaryRegisterMap, SunSpecRegisterMap)

// ProprietaryRegisterMap is the undocumented RS485 protocol of the
// Fronius Smart Meter 63A-3 as polled by the inverter (unit ID 1, 9600 8N1, FC03).
//
// Fronius publishes no specification for this range. The map was obtained in 2020
// by sniffing the RS485 traffic between inverter and a real meter (see emu.yaml on
// primary-meter) and is partially confirmed by the LoxWiki page
// https://loxwiki.atlassian.net/wiki/spaces/LOX/pages/1536590351.
// Keep it bit-identical to the old emulator: types, scaling and word order
// (high word first) are what the inverter accepts. Unlisted registers in the
// range stay 0. The Smart Meter TS 65A-3 uses a different map (from 258 on).
var ProprietaryRegisterMap = []RegisterEntry{
	// Meaning unknown, fixed values seen on the wire (285, 117).
	{Addr: 18, Field: FieldDeviceID, Type: RegUint16, SF: 0},
	{Addr: 768, Field: FieldFirmware, Type: RegUint16, SF: 0},
	{Addr: 4176, Field: FieldSerialNumber, Type: RegUint32, SF: 0},

	{Addr: 4096, Field: FieldVoltageL1, Type: RegUint32, SF: -3},
	{Addr: 4098, Field: FieldVoltageL2, Type: RegUint32, SF: -3},
	{Addr: 4100, Field: FieldVoltageL3, Type: RegUint32, SF: -3},
	{Addr: 4102, Field: FieldCurrentL1, Type: RegUint32, SF: -3},
	{Addr: 4104, Field: FieldCurrentL2, Type: RegUint32, SF: -3},
	{Addr: 4106, Field: FieldCurrentL3, Type: RegUint32, SF: -3},
	{Addr: 4110, Field: FieldVoltageL1L2, Type: RegUint32, SF: -3},
	{Addr: 4112, Field: FieldVoltageL2L3, Type: RegUint32, SF: -3},
	{Addr: 4114, Field: FieldVoltageL3L1, Type: RegUint32, SF: -3},
	{Addr: 4116, Field: FieldPowerTotal, Type: RegInt32, SF: -2},
	{Addr: 4124, Field: FieldEnergyImport, Type: RegUint32, SF: 0},
	{Addr: 4128, Field: FieldEnergyExport, Type: RegUint32, SF: 0},
	{Addr: 4132, Field: FieldPFTotal, Type: RegInt16, SF: -2},
	{Addr: 4134, Field: FieldFrequency, Type: RegUint16, SF: -1},
	{Addr: 4140, Field: FieldPowerL1, Type: RegInt32, SF: -2},
	{Addr: 4142, Field: FieldPowerL2, Type: RegInt32, SF: -2},
	{Addr: 4144, Field: FieldPowerL3, Type: RegInt32, SF: -2},
	{Addr: 4164, Field: FieldPFL1, Type: RegInt16, SF: -2},
	{Addr: 4165, Field: FieldPFL2, Type: RegInt16, SF: -2},
	{Addr: 4166, Field: FieldPFL3, Type: RegInt16, SF: -2},
}

// SunSpecRegisterMap is the SunSpec block of a Fronius Smart Meter as served by
// Fronius over Modbus TCP (unit ID 200), read e.g. by wallboxes or evcc.
//
// Specified by the SunSpec Alliance (models 1 and 203, Int+SF) and by Fronius in
// Smart_Meter_Register_Map_Int&SF.xlsx (gen24-modbus-api-external-docs.zip).
// The documents count from 1; address here = documented register - 1.
var SunSpecRegisterMap = []RegisterEntry{
	// "SunS" marker
	fix(40000, RegUint32, 0x53756E53),

	// Common block (model 1)
	fix(40002, RegUint16, 1),
	fix(40003, RegUint16, 65),
	fix(40004, RegString, "Fronius", 16),
	fix(40020, RegString, "Smart Meter 63A", 16),
	fix(40036, RegString, "", 8),
	fix(40044, RegString, "1.17", 8),
	{Addr: 40052, Field: FieldSerialStr, Type: RegString, NRegs: 16},
	{Addr: 40068, Field: FieldModbusAddr, Type: RegUint16, SF: 0},

	// Meter model 203 (three-phase wye, Int+SF)
	fix(40069, RegUint16, 203),
	fix(40070, RegUint16, 105),

	{Addr: 40071, Field: FieldCurrentTotal, Type: RegInt16, SF: -2},
	{Addr: 40072, Field: FieldCurrentL1, Type: RegInt16, SF: -2},
	{Addr: 40073, Field: FieldCurrentL2, Type: RegInt16, SF: -2},
	{Addr: 40074, Field: FieldCurrentL3, Type: RegInt16, SF: -2},
	fix(40075, RegInt16, -2),

	{Addr: 40076, Field: FieldVoltageAvgPN, Type: RegInt16, SF: -1},
	{Addr: 40077, Field: FieldVoltageL1, Type: RegInt16, SF: -1},
	{Addr: 40078, Field: FieldVoltageL2, Type: RegInt16, SF: -1},
	{Addr: 40079, Field: FieldVoltageL3, Type: RegInt16, SF: -1},
	{Addr: 40080, Field: FieldVoltageAvgPP, Type: RegInt16, SF: -1},
	{Addr: 40081, Field: FieldVoltageL1L2, Type: RegInt16, SF: -1},
	{Addr: 40082, Field: FieldVoltageL2L3, Type: RegInt16, SF: -1},
	{Addr: 40083, Field: FieldVoltageL3L1, Type: RegInt16, SF: -1},
	fix(40084, RegInt16, -1),

	{Addr: 40085, Field: FieldFrequency, Type: RegUint16, SF: -2},
	fix(40086, RegInt16, -2),

	{Addr: 40087, Field: FieldPowerTotal, Type: RegInt16, SF: 0},
	{Addr: 40088, Field: FieldPowerL1, Type: RegInt16, SF: 0},
	{Addr: 40089, Field: FieldPowerL2, Type: RegInt16, SF: 0},
	{Addr: 40090, Field: FieldPowerL3, Type: RegInt16, SF: 0},
	fix(40091, RegInt16, 0),

	{Addr: 40092, Field: FieldApparentTotal, Type: RegInt16, SF: 0},
	{Addr: 40093, Field: FieldApparentL1, Type: RegInt16, SF: 0},
	{Addr: 40094, Field: FieldApparentL2, Type: RegInt16, SF: 0},
	{Addr: 40095, Field: FieldApparentL3, Type: RegInt16, SF: 0},
	fix(40096, RegInt16, 0),

	{Addr: 40097, Field: FieldReactiveTotal, Type: RegInt16, SF: 0},
	{Addr: 40098, Field: FieldReactiveL1, Type: RegInt16, SF: 0},
	{Addr: 40099, Field: FieldReactiveL2, Type: RegInt16, SF: 0},
	{Addr: 40100, Field: FieldReactiveL3, Type: RegInt16, SF: 0},
	fix(40101, RegInt16, 0),

	// PF is published in percent with PF_SF -1, i.e. the register holds PF*1000.
	{Addr: 40102, Field: FieldPFTotal, Type: RegInt16, SF: -3},
	{Addr: 40103, Field: FieldPFL1, Type: RegInt16, SF: -3},
	{Addr: 40104, Field: FieldPFL2, Type: RegInt16, SF: -3},
	{Addr: 40105, Field: FieldPFL3, Type: RegInt16, SF: -3},
	fix(40106, RegInt16, -1),

	{Addr: 40107, Field: FieldEnergyExport, Type: RegUint32, SF: 0},
	{Addr: 40109, Field: FieldEnergyExpL1, Type: RegUint32, SF: 0},
	{Addr: 40111, Field: FieldEnergyExpL2, Type: RegUint32, SF: 0},
	{Addr: 40113, Field: FieldEnergyExpL3, Type: RegUint32, SF: 0},

	{Addr: 40115, Field: FieldEnergyImport, Type: RegUint32, SF: 0},
	{Addr: 40117, Field: FieldEnergyImpL1, Type: RegUint32, SF: 0},
	{Addr: 40119, Field: FieldEnergyImpL2, Type: RegUint32, SF: 0},
	{Addr: 40121, Field: FieldEnergyImpL3, Type: RegUint32, SF: 0},
	fix(40123, RegInt16, 0),

	// SunSpec end block
	fix(40176, RegUint16, 0xFFFF),
	fix(40177, RegUint16, 0),
}

type Snapshot struct {
	numbers map[CanonicalField]float64
	strings map[CanonicalField]string
}

func NewSnapshot() Snapshot {
	return Snapshot{
		numbers: make(map[CanonicalField]float64),
		strings: make(map[CanonicalField]string),
	}
}

func (s *Snapshot) Set(field CanonicalField, value float64) {
	s.numbers[field] = value
}

func (s *Snapshot) SetString(field CanonicalField, value string) {
	s.strings[field] = value
}

func (s Snapshot) Has(field CanonicalField) bool {
	_, ok := s.numbers[field]
	return ok
}

func (s Snapshot) HasString(field CanonicalField) bool {
	_, ok := s.strings[field]
	return ok
}

func (s Snapshot) Float64(field CanonicalField) float64 {
	return s.numbers[field]
}

func (s Snapshot) String(field CanonicalField) string {
	return s.strings[field]
}

type RegisterBank struct {
	regs [65536]uint16
}

func (b *RegisterBank) Uint16(addr int) uint16 { return b.regs[addr] }

func (b *RegisterBank) setUint16(addr int, v uint16) { b.regs[addr] = v }
func (b *RegisterBank) setInt16(addr int, v int16)   { b.regs[addr] = uint16(v) }

func (b *RegisterBank) setUint32(addr int, v uint32) {
	b.regs[addr] = uint16(v >> 16)
	b.regs[addr+1] = uint16(v & 0xFFFF)
}

func (b *RegisterBank) setInt32(addr int, v int32) {
	b.regs[addr] = uint16(uint32(v) >> 16)
	b.regs[addr+1] = uint16(uint32(v) & 0xFFFF)
}

func (b *RegisterBank) setFloat32(addr int, v float32) {
	bits := math.Float32bits(v)
	b.regs[addr] = uint16(bits >> 16)
	b.regs[addr+1] = uint16(bits & 0xFFFF)
}

func (b *RegisterBank) setString(addr, nRegs int, s string) {
	buf := make([]byte, nRegs*2)
	copy(buf, s)
	for i := 0; i < nRegs; i++ {
		b.regs[addr+i] = binary.BigEndian.Uint16(buf[i*2 : i*2+2])
	}
}

func (b *RegisterBank) writeValue(addr int, t RegType, value float64, sf int) error {
	if t == RegFloat32 {
		b.setFloat32(addr, float32(value))
		return nil
	}

	value = math.Round(value / math.Pow10(sf))
	switch t {
	case RegInt16:
		b.setInt16(addr, int16(clamp(value, math.MinInt16, math.MaxInt16)))
	case RegUint16:
		b.setUint16(addr, uint16(clamp(value, 0, math.MaxUint16)))
	case RegInt32:
		b.setInt32(addr, int32(clamp(value, math.MinInt32, math.MaxInt32)))
	case RegUint32:
		b.setUint32(addr, uint32(clamp(value, 0, math.MaxUint32)))
	default:
		return fmt.Errorf("unsupported register type %d at addr %d", t, addr)
	}
	return nil
}

func (b *RegisterBank) WriteSnapshot(s Snapshot) error {
	for _, entry := range FroniusRegisterMap {
		if entry.Type == RegString {
			if entry.Field == "" {
				b.setString(entry.Addr, entry.NRegs, entry.FixString)
			} else {
				b.setString(entry.Addr, entry.NRegs, s.String(entry.Field))
			}
			continue
		}

		value := entry.FixValue
		if entry.Field != "" {
			value = s.Float64(entry.Field)
		}
		if err := b.writeValue(entry.Addr, entry.Type, value, entry.SF); err != nil {
			return err
		}
	}
	return nil
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
