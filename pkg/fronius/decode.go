package fronius

import (
	"bytes"
	"math"
)

// Width returns how many registers the entry occupies.
func (e RegisterEntry) Width() int {
	switch e.Type {
	case RegString:
		return e.NRegs
	case RegUint32, RegInt32, RegFloat32:
		return 2
	default:
		return 1
	}
}

// String names the register type as used in the documentation, e.g. "uint32".
func (t RegType) String() string {
	switch t {
	case RegUint16:
		return "uint16"
	case RegUint32:
		return "uint32"
	case RegInt16:
		return "int16"
	case RegInt32:
		return "int32"
	case RegFloat32:
		return "float32"
	case RegString:
		return "string"
	default:
		return "unknown"
	}
}

// DecodeEntry turns the registers of entry (Width() words, high word first) back into what
// RegisterBank.WriteSnapshot wrote: the scaled value, or the text of a string entry with
// trailing NUL bytes removed.
func DecodeEntry(entry RegisterEntry, regs []uint16) (value float64, text string) {
	if len(regs) < entry.Width() {
		return 0, ""
	}

	var raw float64
	switch entry.Type {
	case RegString:
		buf := make([]byte, 0, 2*len(regs))
		for _, r := range regs[:entry.NRegs] {
			buf = append(buf, byte(r>>8), byte(r))
		}
		return 0, string(bytes.TrimRight(buf, "\x00"))
	case RegFloat32:
		return float64(math.Float32frombits(uint32(regs[0])<<16 | uint32(regs[1]))), ""
	case RegUint16:
		raw = float64(regs[0])
	case RegInt16:
		raw = float64(int16(regs[0]))
	case RegUint32:
		raw = float64(uint32(regs[0])<<16 | uint32(regs[1]))
	case RegInt32:
		raw = float64(int32(uint32(regs[0])<<16 | uint32(regs[1])))
	}
	return raw * math.Pow10(entry.SF), ""
}
