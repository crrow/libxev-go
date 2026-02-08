/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package redisproto

import (
	"fmt"
	"strconv"
)

// Encode serializes a single RESP2 value.
func Encode(v Value) ([]byte, error) {
	return AppendEncode(nil, v)
}

// AppendEncode appends serialized bytes for v into dst.
func AppendEncode(dst []byte, v Value) ([]byte, error) {
	if err := v.validateForEncode(); err != nil {
		return nil, err
	}

	switch v.Kind {
	case KindSimpleString:
		dst = append(dst, '+')
		dst = append(dst, v.Str...)
		dst = append(dst, '\r', '\n')
		return dst, nil
	case KindError:
		dst = append(dst, '-')
		dst = append(dst, v.Str...)
		dst = append(dst, '\r', '\n')
		return dst, nil
	case KindInteger:
		dst = append(dst, ':')
		dst = strconv.AppendInt(dst, v.Int, 10)
		dst = append(dst, '\r', '\n')
		return dst, nil
	case KindBulkString:
		dst = append(dst, '$')
		dst = strconv.AppendInt(dst, int64(len(v.Bulk)), 10)
		dst = append(dst, '\r', '\n')
		dst = append(dst, v.Bulk...)
		dst = append(dst, '\r', '\n')
		return dst, nil
	case KindArray:
		dst = append(dst, '*')
		dst = strconv.AppendInt(dst, int64(len(v.Array)), 10)
		dst = append(dst, '\r', '\n')
		for _, item := range v.Array {
			var err error
			dst, err = AppendEncode(dst, item)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil
	case KindNull:
		return append(dst, '$', '-', '1', '\r', '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported kind: %s", v.Kind)
	}
}
