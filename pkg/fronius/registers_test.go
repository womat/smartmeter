package fronius

import (
	"math"
	"testing"
)

func width(e RegisterEntry) int {
	switch e.Type {
	case RegString:
		return e.NRegs
	case RegUint32, RegInt32, RegFloat32:
		return 2
	default:
		return 1
	}
}

// TestRegisterMapsDoNotOverlap guards against a typo in an address: two entries writing the
// same register would silently overwrite each other.
func TestRegisterMapsDoNotOverlap(t *testing.T) {
	owner := map[int]RegisterEntry{}
	for _, e := range FroniusRegisterMap {
		for a := e.Addr; a < e.Addr+width(e); a++ {
			if other, used := owner[a]; used {
				t.Errorf("register %d used by %+v and %+v", a, other, e)
			}
			owner[a] = e
		}
	}
	if len(FroniusRegisterMap) != len(ProprietaryRegisterMap)+len(SunSpecRegisterMap) {
		t.Error("FroniusRegisterMap is not the union of both maps")
	}
}

// TestSunSpecBlockLengths checks the model lengths against the addresses: a client walks the
// SunSpec chain by ID and length, so a wrong length hides the meter model or the end block.
func TestSunSpecBlockLengths(t *testing.T) {
	var bank RegisterBank
	if err := bank.WriteSnapshot(NewSnapshot()); err != nil {
		t.Fatal(err)
	}

	if hi, lo := bank.Uint16(40000), bank.Uint16(40001); hi != 0x5375 || lo != 0x6E53 {
		t.Fatalf("40000 = %#x %#x, want SunS", hi, lo)
	}
	addr := 40002
	for _, model := range []struct {
		id     uint16
		length uint16
	}{{1, 65}, {203, 105}} {
		if id, length := bank.Uint16(addr), bank.Uint16(addr+1); id != model.id || length != model.length {
			t.Fatalf("model at %d = %d/%d, want %d/%d", addr, id, length, model.id, model.length)
		}
		addr += 2 + int(model.length)
	}
	if id, length := bank.Uint16(addr), bank.Uint16(addr+1); id != 0xFFFF || length != 0 {
		t.Errorf("end block at %d = %#x/%d, want 0xffff/0", addr, id, length)
	}
}

func TestWriteSnapshotEncoding(t *testing.T) {
	s := NewSnapshot()
	s.Set(FieldPowerTotal, -16)        // int32, SF -2
	s.Set(FieldEnergyImport, 66517754) // uint32, high word first
	s.Set(FieldVoltageL1, 229.1)       // uint32, SF -3
	s.Set(FieldPFL1, 0.82)             // int16, SF -2 (4164) and -3 (40103)
	s.Set(FieldPowerL1, 1e9)           // int16 at 40088: clamped
	s.Set(FieldCurrentL1, -5)          // uint32 at 4102: clamped to 0
	s.SetString(FieldSerialStr, "12345678")

	var bank RegisterBank
	if err := bank.WriteSnapshot(s); err != nil {
		t.Fatal(err)
	}
	u32 := func(a int) uint32 { return uint32(bank.Uint16(a))<<16 | uint32(bank.Uint16(a+1)) }

	if got := int32(u32(4116)); got != -1600 {
		t.Errorf("4116 = %d, want -1600", got)
	}
	if got := u32(4124); got != 66517754 {
		t.Errorf("4124 = %d, want 66517754", got)
	}
	if got := u32(4096); got != 229100 {
		t.Errorf("4096 = %d, want 229100", got)
	}
	if got := int16(bank.Uint16(4164)); got != 82 {
		t.Errorf("4164 = %d, want 82", got)
	}
	if got := int16(bank.Uint16(40103)); got != 820 {
		t.Errorf("40103 = %d, want 820", got)
	}
	if got := int16(bank.Uint16(40088)); got != math.MaxInt16 {
		t.Errorf("40088 = %d, want clamped to %d", got, math.MaxInt16)
	}
	if got := u32(4102); got != 0 {
		t.Errorf("4102 = %d, want negative current clamped to 0", got)
	}
	if hi, lo := bank.Uint16(40052), bank.Uint16(40053); hi != 0x3132 || lo != 0x3334 {
		t.Errorf("40052 = %#x %#x, want \"1234\"", hi, lo)
	}
	if got := bank.Uint16(40060); got != 0 {
		t.Errorf("40060 = %#x, want the string padded with zeros", got)
	}
}

func TestSnapshot(t *testing.T) {
	s := NewSnapshot()
	if s.Has(FieldFrequency) || s.HasString(FieldSerialStr) {
		t.Error("a new snapshot has values")
	}
	s.Set(FieldFrequency, 50)
	s.SetString(FieldSerialStr, "x")
	if !s.Has(FieldFrequency) || s.Float64(FieldFrequency) != 50 || !s.HasString(FieldSerialStr) || s.String(FieldSerialStr) != "x" {
		t.Error("snapshot does not return what was set")
	}
}
