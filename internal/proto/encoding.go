package proto

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
)

// inlined math.MaxUint32 to avoid package import.
const maxUint32 = 1<<32 - 1

const errVarintOverflow = "varint overflow"

// EncodeTag encodes protobuf tag for field with given number and type.
func EncodeTag(num, typ uint64) uint64 {
	return num<<3 | typ&7
}

// SizeTag returns size of protobuf tag for field with given number.
func SizeTag(num uint64) int {
	return SizeVarint(num << 3)
}

// SizeLEN returns length of nested [FieldTypeLEN] field.
func SizeLEN(ln int) int {
	return SizeVarint(uint64(ln)) + ln
}

// SizeVarint returns length of [FieldTypeVARINT] field.
func SizeVarint(x uint64) int {
	i := 0
	for x >= 0x80 {
		x >>= 7
		i++
	}
	return i + 1
}

// PutUvarint encodes x into buf with given offset and returns the number of
// bytes written.
func PutUvarint(buf []byte, off int, x uint64) int {
	i := 0
	for x >= 0x80 {
		// &0xFF is the only difference with binary.PutUvarint needed because
		// VM throws exception on type narrowing.
		buf[off+i] = byte(x&0xFF | 0x80)
		x >>= 7
		i++
	}
	buf[off+i] = byte(x)
	return i + 1
}

// ReadTag reads tag of protobuf field from b. Returns field number, type,
// number of bytes read and exception.
func ReadTag(b []byte) (int, int, int, string) {
	n, r, e := uvarint(b)
	if e != "" {
		return 0, 0, 0, e
	}

	num := n >> 3
	if num == 0 || num > MaxFieldNumber {
		return 0, 0, 0, "invalid/unsupported protobuf field num " + std.Itoa10(int(n))
	}

	typ := n & 7
	if typ >= firstUnknownFieldType {
		return 0, 0, 0, "invalid/unsupported protobuf field type " + std.Itoa10(int(typ))
	}

	return int(num), int(typ), r, ""
}

// ReadSizeLEN reads length of nested [FieldTypeLEN] field from b. Returns
// resulting length, number of bytes read and exception.
func ReadSizeLEN(b []byte) (int, int, string) {
	n, r, e := uvarint(b)
	if e != "" {
		return 0, 0, e
	}

	if full := uint64(len(b)); n > full || n > full-uint64(r) {
		return 0, 0, "too big field len " + std.Itoa10(int(n)) + ", full " + std.Itoa10(len(b))
	}

	return int(n), r, ""
}

// ReadUint32 reads protobuf field of uint32 type. Returns field value, number
// of bytes read and exception.
func ReadUint32(b []byte) (uint32, int, string) {
	n, r, e := uvarint(b)
	if e != "" {
		return 0, 0, e
	}

	if n > maxUint32 {
		return 0, 0, std.Itoa10(int(n)) + " overflows uint32"
	}

	return uint32(n), r, ""
}

func uvarint(buf []byte) (uint64, int, string) {
	const maxVarintLen = 10
	var x uint64
	var s uint
	for i, b := range buf {
		if i == maxVarintLen {
			// Catch byte reads past maxVarintLen.
			// See issue https://golang.org/issues/41185
			return 0, 0, errVarintOverflow
		}
		if b < 0x80 {
			if i == maxVarintLen-1 && b > 1 {
				return 0, 0, errVarintOverflow
			}
			return x | uint64(b)<<s, i + 1, ""
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, 0, ""
}
