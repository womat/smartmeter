package fronius

import (
	"math"
	"testing"
)

// TestDecodeEntryRoundTrip writes a snapshot and decodes every entry of both maps back: each value
// must come back exactly, fixed values and strings too.
func TestDecodeEntryRoundTrip(t *testing.T) {
	// A field can sit in both maps with different types and scale factors (voltage_l1 is uint32
	// SF -3 at 4096 and int16 SF -1 at 40077), so every field gets one small positive integer:
	// exact at every scale factor and in range of every type.
	s := NewSnapshot()
	for i, entry := range FroniusRegisterMap {
		switch {
		case entry.Field == "":
		case entry.Type == RegString:
			s.SetString(entry.Field, "SN-1234")
		case !s.Has(entry.Field):
			s.Set(entry.Field, float64(1+i%20))
		}
	}

	var bank RegisterBank
	if err := bank.WriteSnapshot(s); err != nil {
		t.Fatal(err)
	}

	for _, entry := range FroniusRegisterMap {
		regs := make([]uint16, entry.Width())
		for i := range regs {
			regs[i] = bank.Uint16(entry.Addr + i)
		}
		value, text := DecodeEntry(entry, regs)

		switch {
		case entry.Type == RegString && entry.Field == "":
			if text != entry.FixString {
				t.Errorf("%d: text %q, want %q", entry.Addr, text, entry.FixString)
			}
		case entry.Type == RegString:
			if text != s.String(entry.Field) {
				t.Errorf("%d %s: text %q, want %q", entry.Addr, entry.Field, text, s.String(entry.Field))
			}
		case entry.Field == "":
			if value != entry.FixValue {
				t.Errorf("%d: fixed value %g, want %g", entry.Addr, value, entry.FixValue)
			}
		default:
			if want := s.Float64(entry.Field); math.Abs(value-want) > 1e-9 {
				t.Errorf("%d %s: value %g, want %g", entry.Addr, entry.Field, value, want)
			}
		}
	}
}

func TestDecodeEntryShortInput(t *testing.T) {
	if v, s := DecodeEntry(RegisterEntry{Type: RegUint32}, []uint16{1}); v != 0 || s != "" {
		t.Errorf("DecodeEntry with one register for uint32 = %g, %q, want zero values", v, s)
	}
}

func TestRegType(t *testing.T) {
	if RegInt32.String() != "int32" || RegString.String() != "string" || RegType(99).String() != "unknown" {
		t.Error("RegType.String() names")
	}
	if (RegisterEntry{Type: RegString, NRegs: 16}).Width() != 16 || (RegisterEntry{Type: RegFloat32}).Width() != 2 {
		t.Error("RegisterEntry.Width()")
	}
}
