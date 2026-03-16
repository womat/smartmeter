package main

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/womat/golib/keyvalue"
)

// ---------------------------------------------------------------------------
// Kanonische Felder
// ---------------------------------------------------------------------------

// CanonicalField ist der eindeutige Name eines Messwerts.
// Alle Werte werden intern als float64 in SI-Einheiten geführt:
//
//	Spannung  [V], Strom [A], Leistung [W], Energie [Wh], Frequenz [Hz], PF [-]
type CanonicalField string

const (

	// Proprietäre Fronius-Identifikationsregister — type: fixed in config.yaml
	FieldDeviceID     CanonicalField = "device_id"     // Addr 18,   uint16, fix: 285
	FieldFirmware     CanonicalField = "firmware"      // Addr 768,  uint16, fix: 117
	FieldSerialNumber CanonicalField = "serial_number" // Addr 4176, uint32, fix: 99999999
	FieldSerialStr    CanonicalField = "serial_str"    // string

	// SunSpec Common Block — type: fixed in config.yaml
	FieldSunSpecMagic CanonicalField = "sunspec_magic" // Addr 40000, uint32, fix: 0x53756E53
	FieldModbusAddr   CanonicalField = "modbus_addr"   // Addr 40068, uint16, = slave_id

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

	// Abgeleitete Felder — werden nicht direkt aus dem Quellzähler gelesen,
	// sondern durch den Polling-Mechanismus aus anderen Feldern berechnet
	// und dann wie alle anderen in den Snapshot geschrieben.
	FieldCurrentTotal  CanonicalField = "current_total"
	FieldVoltageAvgPN  CanonicalField = "voltage_avg_pn" // Mittelwert Ph–N
	FieldVoltageAvgPP  CanonicalField = "voltage_avg_pp" // Mittelwert Ph–Ph
	FieldVoltageL1L2   CanonicalField = "voltage_l1_l2"
	FieldVoltageL2L3   CanonicalField = "voltage_l2_l3"
	FieldVoltageL3L1   CanonicalField = "voltage_l3_l1"
	FieldApparentTotal CanonicalField = "apparent_total" // Scheinleistung [VA]
	FieldApparentL1    CanonicalField = "apparent_l1"
	FieldApparentL2    CanonicalField = "apparent_l2"
	FieldApparentL3    CanonicalField = "apparent_l3"
	FieldReactiveTotal CanonicalField = "reactive_total" // Blindleistung [VAr]
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

// RequiredFields müssen im Config-Map vorhanden sein (direkt oder als expr).
var RequiredFields = []CanonicalField{
	FieldPowerTotal,
	FieldPowerL1,
	FieldPowerL2,
	FieldPowerL3,
	FieldVoltageL1,
	FieldVoltageL2,
	FieldVoltageL3,
	FieldCurrentL1,
	FieldCurrentL2,
	FieldCurrentL3,
	FieldEnergyImport,
	FieldEnergyExport,
	FieldFrequency,
	FieldDeviceID,
	FieldFirmware,
	FieldSerialNumber,
	FieldSunSpecMagic,
	FieldModbusAddr,
}

// ---------------------------------------------------------------------------
// Registertypen
// ---------------------------------------------------------------------------

type RegType uint8

const (
	RegUint16  RegType = iota // 1 Register, 16 Bit, unsigned, 					Big-Endian Byte-Order
	RegUint32                 // 2 Register, 32 Bit, unsigned,					Big-Endian Byte-Order, Big-Endian Word-Order
	RegInt16                  // 1 Register, 16 Bit, signed (two's complement),	Big-Endian Byte-Order
	RegInt32                  // 2 Register, 32 Bit, signed (two's complement), 	Big-Endian Byte-Order, Big-Endian Word-Order
	RegFloat32                // 2 Register, 32 Bit, IEEE 754 single,Big-Endian 	Byte-Order, Big-Endian Word-Order
	RegString                 // NRegs Register, ASCII, 2 Bytes pro Register, 	Big-Endian Byte-Order, null-padded
)

// ---------------------------------------------------------------------------
// Mapping-Tabelle: kanonisches Feld → Fronius-Zielregister
//
// Jeder Eintrag beschreibt:
//   Addr     0-basierte Modbus-Adresse im Emulator
//   Field    kanonisches Feld aus dem Snapshot
//   Type     Registertyp (bestimmt Größe und Vorzeichen)
//   SF       Skalierungsfaktor-Exponent: Rohwert = Realwert / 10^SF
//            → Register enthält: round(Realwert * 10^(-SF))
//
// Berechnungen wie Scheinleistung, √3-Spannung, Phasenenergie etc.
// sind NICHT hier — sie stehen als abgeleitete Felder im Snapshot.
// ---------------------------------------------------------------------------

type registerEntry struct {
	Addr      int
	Field     CanonicalField // "" = fixer Wert
	Type      RegType
	SF        int     // Exponent: Rohwert = round(Realwert / 10^SF)
	NRegs     int     // nur für RegString
	FixValue  float64 // nur wenn Field == "" und Type != RegString
	FixString string  // nur wenn Field == "" und Type == RegString
}

// fix erstellt einen Eintrag mit fixem Wert.
// val kann float64, int, oder string sein:
//
//	fix(40000, RegUint32, 0x53756E53)
//	fix(40075, RegInt16, -2)
//	fix(40004, RegString, "Fronius", 16)  ← nRegs als letztes Argument bei RegString
func fix(addr int, t RegType, val any, nRegs ...int) registerEntry {
	e := registerEntry{Addr: addr, Type: t}
	if t == RegString {
		e.FixString, _ = val.(string)
		if len(nRegs) > 0 {
			e.NRegs = nRegs[0]
		}
	} else {
		switch v := val.(type) {
		case float64:
			e.FixValue = v
		case int:
			e.FixValue = float64(v)
		}
	}
	return e
}

// ---------------------------------------------------------------------------
// Fronius Mapping-Tabelle — einzige Quelle der Wahrheit
//
// Reihenfolge: proprietäre Register → Common Block → Meter Block
// SF = 0  bedeutet: Rohwert = Realwert direkt (keine Umrechnung)
// ---------------------------------------------------------------------------

var froniusRegisterMap = []registerEntry{

	// -- Proprietäre Identifikationsregister ---------------------------------
	{Addr: 18, Field: FieldDeviceID, Type: RegUint16, SF: 0},
	{Addr: 768, Field: FieldFirmware, Type: RegUint16, SF: 0},
	{Addr: 4176, Field: FieldSerialNumber, Type: RegUint32, SF: 0},

	// -- SunSpec Common Block ------------------------------------------------
	fix(40000, RegUint32, 0x53756E53),                                // SunSpec Magic "SunS"
	fix(40002, RegUint16, 1),                                         // Common Model ID
	fix(40003, RegUint16, 65),                                        // Common Block Length
	fix(40004, RegString, "Fronius", 16),                             // Manufacturer        string[32]
	fix(40020, RegString, "Smart Meter TS65A-3", 16),                 // Device Model  string[32]
	fix(40036, RegString, "", 8),                                     // Options       string[16]
	fix(40044, RegString, "1.17", 8),                                 // SW Version    string[16]
	{Addr: 40052, Field: FieldSerialStr, Type: RegString, NRegs: 16}, // string[32]
	{Addr: 40068, Field: FieldModbusAddr, Type: RegUint16, SF: 0},

	// -- SunSpec Meter Block (Modell 203, int+SF) ----------------------------
	fix(40069, RegUint16, 203), // Meter Model ID
	fix(40070, RegUint16, 105), // Meter Block Length

	// Ströme [A] → SF -2 (Rohwert in 10mA)
	{Addr: 40071, Field: FieldCurrentTotal, Type: RegInt16, SF: -2},
	{Addr: 40072, Field: FieldCurrentL1, Type: RegInt16, SF: -2},
	{Addr: 40073, Field: FieldCurrentL2, Type: RegInt16, SF: -2},
	{Addr: 40074, Field: FieldCurrentL3, Type: RegInt16, SF: -2},
	fix(40075, RegInt16, -2), // A_SF

	// Spannungen Phase–Neutral [V] → SF -1 (Rohwert in 100mV)
	{Addr: 40076, Field: FieldVoltageAvgPN, Type: RegInt16, SF: -1},
	{Addr: 40077, Field: FieldVoltageL1, Type: RegInt16, SF: -1},
	{Addr: 40078, Field: FieldVoltageL2, Type: RegInt16, SF: -1},
	{Addr: 40079, Field: FieldVoltageL3, Type: RegInt16, SF: -1},

	// Spannungen Phase–Phase [V] → SF -1
	{Addr: 40080, Field: FieldVoltageAvgPP, Type: RegInt16, SF: -1},
	{Addr: 40081, Field: FieldVoltageL1L2, Type: RegInt16, SF: -1},
	{Addr: 40082, Field: FieldVoltageL2L3, Type: RegInt16, SF: -1},
	{Addr: 40083, Field: FieldVoltageL3L1, Type: RegInt16, SF: -1},
	fix(40084, RegInt16, -1), // V_SF

	// Frequenz [Hz] → SF -2 (Rohwert in 10mHz)
	{Addr: 40085, Field: FieldFrequency, Type: RegUint16, SF: -2},
	fix(40086, RegInt16, -2), // Hz_SF

	// Wirkleistung [W] → SF 0
	{Addr: 40087, Field: FieldPowerTotal, Type: RegInt16, SF: 0},
	{Addr: 40088, Field: FieldPowerL1, Type: RegInt16, SF: 0},
	{Addr: 40089, Field: FieldPowerL2, Type: RegInt16, SF: 0},
	{Addr: 40090, Field: FieldPowerL3, Type: RegInt16, SF: 0},
	fix(40091, RegInt16, 0), // W_SF

	// Scheinleistung [VA] → SF 0
	{Addr: 40092, Field: FieldApparentTotal, Type: RegInt16, SF: 0},
	{Addr: 40093, Field: FieldApparentL1, Type: RegInt16, SF: 0},
	{Addr: 40094, Field: FieldApparentL2, Type: RegInt16, SF: 0},
	{Addr: 40095, Field: FieldApparentL3, Type: RegInt16, SF: 0},
	fix(40096, RegInt16, 0), // VA_SF

	// Blindleistung [VAr] → SF 0
	{Addr: 40097, Field: FieldReactiveTotal, Type: RegInt16, SF: 0},
	{Addr: 40098, Field: FieldReactiveL1, Type: RegInt16, SF: 0},
	{Addr: 40099, Field: FieldReactiveL2, Type: RegInt16, SF: 0},
	{Addr: 40100, Field: FieldReactiveL3, Type: RegInt16, SF: 0},
	fix(40101, RegInt16, 0), // VAR_SF

	// Leistungsfaktor [-1.0..1.0] → SF -3 (Rohwert in 0.001)
	{Addr: 40102, Field: FieldPFTotal, Type: RegInt16, SF: -3},
	{Addr: 40103, Field: FieldPFL1, Type: RegInt16, SF: -3},
	{Addr: 40104, Field: FieldPFL2, Type: RegInt16, SF: -3},
	{Addr: 40105, Field: FieldPFL3, Type: RegInt16, SF: -3},
	fix(40106, RegInt16, -3), // PF_SF

	// Energie Export [Wh] → SF 0
	{Addr: 40107, Field: FieldEnergyExport, Type: RegUint32, SF: 0},
	{Addr: 40109, Field: FieldEnergyExpL1, Type: RegUint32, SF: 0},
	{Addr: 40111, Field: FieldEnergyExpL2, Type: RegUint32, SF: 0},
	{Addr: 40113, Field: FieldEnergyExpL3, Type: RegUint32, SF: 0},

	// Energie Import [Wh] → SF 0
	{Addr: 40115, Field: FieldEnergyImport, Type: RegUint32, SF: 0},
	{Addr: 40117, Field: FieldEnergyImpL1, Type: RegUint32, SF: 0},
	{Addr: 40119, Field: FieldEnergyImpL2, Type: RegUint32, SF: 0},
	{Addr: 40121, Field: FieldEnergyImpL3, Type: RegUint32, SF: 0},
	fix(40123, RegInt16, 0), // TotWh_SF

	// Events
	fix(40126, RegUint32, 0),
}

// ---------------------------------------------------------------------------
// RegisterBank
// ---------------------------------------------------------------------------

type RegisterBank struct {
	regs [65536]uint16
}

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
	for i := range nRegs {
		b.regs[addr+i] = binary.BigEndian.Uint16(buf[i*2 : i*2+2])
	}
}

func (b *RegisterBank) writeStr(addr, nRegs int, s string) {
	b.setString(addr, nRegs, s)
}

func (b *RegisterBank) writeVal(addr int, t RegType, f float64, sf int) error {

	// float32 kein clamp, kein round — float bleibt float
	if t == RegFloat32 {
		b.setFloat32(addr, float32(f))
		return nil
	}

	f = math.Round(f / math.Pow10(sf))
	switch t {
	case RegInt16:
		b.setInt16(addr, int16(clamp(f, math.MinInt16, math.MaxInt16)))
	case RegUint16:
		b.setUint16(addr, uint16(clamp(f, 0, math.MaxUint16)))
	case RegInt32:
		b.setInt32(addr, int32(clamp(f, math.MinInt32, math.MaxInt32)))
	case RegUint32:
		b.setUint32(addr, uint32(clamp(f, 0, math.MaxUint32)))
	default:
		return fmt.Errorf("unbekannter RegType %d addr %d", t, addr)
	}
	return nil
}

// WriteSnapshot — schreibt alle Felder aus dem Record in die RegisterBank.
// Skalierung und Rundung erfolgen in writeVal.
func (b *RegisterBank) WriteSnapshot(s keyvalue.Record) error {

	getFloat := func(r registerEntry) float64 {
		if r.Field == "" {
			return r.FixValue
		}
		return s.Float64(string(r.Field))
	}

	getStr := func(r registerEntry) string {
		if r.Field == "" {
			return r.FixString
		}
		return s.String(string(r.Field))
	}

	for _, e := range froniusRegisterMap {
		if e.Type == RegString {

			b.writeStr(e.Addr, e.NRegs, getStr(e))
			continue
		}

		if err := b.writeVal(e.Addr, e.Type, getFloat(e), e.SF); err != nil {
			return err
		}

	}
	return nil
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
