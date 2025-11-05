package proto

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
)

// MaxFieldNumber is a maximum field number according to
// https://protobuf.dev/programming-guides/proto3/#assigning.
const MaxFieldNumber = 1<<29 - 1

// All possible field types declared in https://protobuf.dev/programming-guides/encoding/#structure.
const (
	FieldTypeVARINT = iota
	FieldTypeI64
	FieldTypeLEN
	FieldTypeSGROUP
	FieldTypeEGROUP
	FieldTypeI32
	firstUnknownFieldType
)

// StringifyFieldType stringifies given field type.
func StringifyFieldType(typ int) string {
	switch typ {
	case FieldTypeVARINT:
		return "VARINT"
	case FieldTypeI64:
		return "I64"
	case FieldTypeLEN:
		return "LEN"
	case FieldTypeSGROUP:
		return "SGROUP"
	case FieldTypeEGROUP:
		return "EGROUP"
	case FieldTypeI32:
		return "I32"
	default:
		return "UNKNOWN#" + std.Itoa10(typ)
	}
}
