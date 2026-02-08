/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package redisproto

import (
	"bytes"
	"fmt"
	"strconv"
)

const defaultMaxBulkLen = 512 << 20 // 512 MiB
const defaultMaxArrayLen = 1 << 20  // 1M elements
const defaultMaxDepth = 64

// Parser incrementally parses RESP2 frames from streaming input.
type Parser struct {
	buf         []byte
	maxBulkLen  int
	maxArrayLen int
	maxDepth    int
}

// NewParser creates a parser with safe default limits.
func NewParser() *Parser {
	return &Parser{
		maxBulkLen:  defaultMaxBulkLen,
		maxArrayLen: defaultMaxArrayLen,
		maxDepth:    defaultMaxDepth,
	}
}

// Feed appends incoming bytes and returns all fully decoded frames.
// It keeps incomplete tails in parser state for the next call.
func (p *Parser) Feed(in []byte) ([]Value, error) {
	if len(in) > 0 {
		p.buf = append(p.buf, in...)
	}

	if len(p.buf) == 0 {
		return nil, nil
	}

	out := make([]Value, 0, 1)
	offset := 0

	for offset < len(p.buf) {
		v, next, complete, err := p.parseAt(p.buf, offset, 0)
		if err != nil {
			p.buf = p.buf[:0]
			return nil, err
		}
		if !complete {
			break
		}
		out = append(out, v)
		offset = next
	}

	if offset == len(p.buf) {
		p.buf = p.buf[:0]
	} else if offset > 0 {
		p.buf = append([]byte(nil), p.buf[offset:]...)
	}

	return out, nil
}

func (p *Parser) parseAt(data []byte, offset, depth int) (Value, int, bool, error) {
	if depth > p.maxDepth {
		return Value{}, 0, false, fmt.Errorf("array nesting exceeds max depth %d", p.maxDepth)
	}
	if offset >= len(data) {
		return Value{}, 0, false, nil
	}

	prefix := data[offset]
	offset++

	switch prefix {
	case '+', '-', ':':
		line, next, ok := readLine(data, offset)
		if !ok {
			return Value{}, 0, false, nil
		}
		switch prefix {
		case '+':
			return Value{Kind: KindSimpleString, Str: string(line)}, next, true, nil
		case '-':
			return Value{Kind: KindError, Str: string(line)}, next, true, nil
		default:
			n, err := strconv.ParseInt(string(line), 10, 64)
			if err != nil {
				return Value{}, 0, false, fmt.Errorf("invalid integer %q: %w", string(line), err)
			}
			return Value{Kind: KindInteger, Int: n}, next, true, nil
		}
	case '$':
		line, next, ok := readLine(data, offset)
		if !ok {
			return Value{}, 0, false, nil
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return Value{}, 0, false, fmt.Errorf("invalid bulk string length %q: %w", string(line), err)
		}
		if n == -1 {
			return Value{Kind: KindNull}, next, true, nil
		}
		if n < -1 {
			return Value{}, 0, false, fmt.Errorf("negative bulk string length: %d", n)
		}
		if n > int64(p.maxBulkLen) {
			return Value{}, 0, false, fmt.Errorf("bulk string length %d exceeds limit %d", n, p.maxBulkLen)
		}

		need := next + int(n) + 2
		if need > len(data) {
			return Value{}, 0, false, nil
		}
		if data[next+int(n)] != '\r' || data[next+int(n)+1] != '\n' {
			return Value{}, 0, false, fmt.Errorf("bulk string missing CRLF terminator")
		}

		bulk := append([]byte(nil), data[next:next+int(n)]...)
		if n == 0 {
			bulk = []byte{}
		}
		return Value{Kind: KindBulkString, Bulk: bulk}, need, true, nil
	case '*':
		line, next, ok := readLine(data, offset)
		if !ok {
			return Value{}, 0, false, nil
		}

		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return Value{}, 0, false, fmt.Errorf("invalid array length %q: %w", string(line), err)
		}
		if n < 0 {
			return Value{}, 0, false, fmt.Errorf("negative array length: %d", n)
		}
		if n > int64(p.maxArrayLen) {
			return Value{}, 0, false, fmt.Errorf("array length %d exceeds limit %d", n, p.maxArrayLen)
		}

		arr := make([]Value, 0, int(n))
		cursor := next
		for i := int64(0); i < n; i++ {
			item, itemNext, complete, parseErr := p.parseAt(data, cursor, depth+1)
			if parseErr != nil {
				return Value{}, 0, false, parseErr
			}
			if !complete {
				return Value{}, 0, false, nil
			}
			arr = append(arr, item)
			cursor = itemNext
		}
		return Value{Kind: KindArray, Array: arr}, cursor, true, nil
	default:
		return Value{}, 0, false, fmt.Errorf("unknown RESP2 prefix byte %q", prefix)
	}
}

func readLine(data []byte, offset int) ([]byte, int, bool) {
	if offset >= len(data) {
		return nil, 0, false
	}
	i := bytes.Index(data[offset:], []byte("\r\n"))
	if i < 0 {
		return nil, 0, false
	}
	end := offset + i
	return data[offset:end], end + 2, true
}
