// Package scan contains the OS-independent value-matching logic and the
// parallel scan driver used to search a target process's memory. None of the
// code here performs any system calls: memory access is abstracted behind the
// Reader interface, which keeps the hot path testable on any platform.
package scan

import (
	"fmt"
	"math"
	"strconv"
)

// Kind identifies the numeric type of a value being scanned for.
type Kind uint8

// Supported value kinds. The default is KindInt32.
const (
	KindInt32 Kind = iota
	KindUint32
	KindInt64
	KindFloat32
	KindFloat64
)

// ParseKind maps a --type flag string to a Kind.
func ParseKind(s string) (Kind, error) {
	switch s {
	case "int32":
		return KindInt32, nil
	case "uint32":
		return KindUint32, nil
	case "int64":
		return KindInt64, nil
	case "float32":
		return KindFloat32, nil
	case "float64":
		return KindFloat64, nil
	default:
		return 0, fmt.Errorf("unknown type %q (want int32, uint32, int64, float32, float64)", s)
	}
}

// String returns the flag-style name of the kind.
func (k Kind) String() string {
	switch k {
	case KindInt32:
		return "int32"
	case KindUint32:
		return "uint32"
	case KindInt64:
		return "int64"
	case KindFloat32:
		return "float32"
	case KindFloat64:
		return "float64"
	default:
		return "invalid"
	}
}

// Size returns the width of the kind in bytes (4 or 8).
func (k Kind) Size() int {
	switch k {
	case KindInt32, KindUint32, KindFloat32:
		return 4
	case KindInt64, KindFloat64:
		return 8
	default:
		return 0
	}
}

// Value is a parsed scan value: a kind plus its raw little-endian bit pattern
// stored in the low Size() bytes of Bits. Storing the bit pattern lets the hot
// path compare integers and floats uniformly as fixed-width words.
type Value struct {
	Kind Kind
	Bits uint64
}

// Parse interprets s as a value of kind k. Integer inputs accept 0x/0o/0b
// prefixes; negative integers are encoded as their two's-complement bits.
func Parse(k Kind, s string) (Value, error) {
	switch k {
	case KindInt32:
		v, err := strconv.ParseInt(s, 0, 32)
		if err != nil {
			return Value{}, fmt.Errorf("parse int32 %q: %w", s, err)
		}
		return Value{Kind: k, Bits: uint64(uint32(int32(v)))}, nil
	case KindUint32:
		v, err := strconv.ParseUint(s, 0, 32)
		if err != nil {
			return Value{}, fmt.Errorf("parse uint32 %q: %w", s, err)
		}
		return Value{Kind: k, Bits: uint64(uint32(v))}, nil
	case KindInt64:
		v, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return Value{}, fmt.Errorf("parse int64 %q: %w", s, err)
		}
		return Value{Kind: k, Bits: uint64(v)}, nil
	case KindFloat32:
		v, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return Value{}, fmt.Errorf("parse float32 %q: %w", s, err)
		}
		return Value{Kind: k, Bits: uint64(math.Float32bits(float32(v)))}, nil
	case KindFloat64:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return Value{}, fmt.Errorf("parse float64 %q: %w", s, err)
		}
		return Value{Kind: k, Bits: math.Float64bits(v)}, nil
	default:
		return Value{}, fmt.Errorf("invalid kind %d", k)
	}
}

// Decode reads a Value of kind k from the first k.Size() bytes of buf, which
// must be at least that long.
func Decode(k Kind, buf []byte) Value {
	if k.Size() == 4 {
		return Value{Kind: k, Bits: uint64(leU32(buf))}
	}
	return Value{Kind: k, Bits: leU64(buf)}
}

// String formats the value according to its kind.
func (v Value) String() string {
	switch v.Kind {
	case KindInt32:
		return strconv.FormatInt(int64(int32(uint32(v.Bits))), 10)
	case KindUint32:
		return strconv.FormatUint(uint64(uint32(v.Bits)), 10)
	case KindInt64:
		return strconv.FormatInt(int64(v.Bits), 10)
	case KindFloat32:
		return strconv.FormatFloat(float64(math.Float32frombits(uint32(v.Bits))), 'g', -1, 32)
	case KindFloat64:
		return strconv.FormatFloat(math.Float64frombits(v.Bits), 'g', -1, 64)
	default:
		return "?"
	}
}

// Bytes returns the little-endian encoding of the value (Size() bytes).
func (v Value) Bytes() []byte {
	b := make([]byte, v.Kind.Size())
	if len(b) == 4 {
		putU32(b, uint32(v.Bits))
	} else {
		putU64(b, v.Bits)
	}
	return b
}

// Equal reports whether v and other have identical bit patterns. For floats
// this is exact-bit equality (NaN payloads included), which is what an exact
// memory scan wants.
func (v Value) Equal(other Value) bool {
	return v.Bits == other.Bits
}

// Cmp orders v against other using their kind's natural ordering and returns
// -1, 0, or +1. Both values must share the same kind. Float comparisons follow
// IEEE-754, so NaN compares as not-less, not-greater, and not-equal; Cmp
// reports +1 in that case to keep a total order for callers.
func (v Value) Cmp(other Value) int {
	switch v.Kind {
	case KindInt32:
		return cmp(int32(uint32(v.Bits)), int32(uint32(other.Bits)))
	case KindUint32:
		return cmp(uint32(v.Bits), uint32(other.Bits))
	case KindInt64:
		return cmp(int64(v.Bits), int64(other.Bits))
	case KindFloat32:
		return cmpFloat(float64(math.Float32frombits(uint32(v.Bits))), float64(math.Float32frombits(uint32(other.Bits))))
	case KindFloat64:
		return cmpFloat(math.Float64frombits(v.Bits), math.Float64frombits(other.Bits))
	default:
		return 0
	}
}

func cmp[T int32 | uint32 | int64 | uint64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	case a == b:
		return 0
	default:
		return 1 // NaN: keep a total order by treating it as greatest
	}
}
