/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package redisproto

import "fmt"

// Kind identifies RESP2 value types supported by MVP.
type Kind int

const (
	KindSimpleString Kind = iota
	KindError
	KindInteger
	KindBulkString
	KindArray
	KindNull
)

// Value is a typed RESP2 value.
type Value struct {
	Kind  Kind
	Str   string
	Int   int64
	Bulk  []byte
	Array []Value
}

func (k Kind) String() string {
	switch k {
	case KindSimpleString:
		return "simple_string"
	case KindError:
		return "error"
	case KindInteger:
		return "integer"
	case KindBulkString:
		return "bulk_string"
	case KindArray:
		return "array"
	case KindNull:
		return "null"
	default:
		return "unknown"
	}
}

func (v Value) validateForEncode() error {
	switch v.Kind {
	case KindSimpleString, KindError:
		if hasRESPNewline(v.Str) {
			return fmt.Errorf("%s contains CR or LF", v.Kind)
		}
		return nil
	case KindInteger, KindBulkString, KindArray, KindNull:
		return nil
	default:
		return fmt.Errorf("unsupported kind: %d", v.Kind)
	}
}

func hasRESPNewline(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' || s[i] == '\n' {
			return true
		}
	}
	return false
}
