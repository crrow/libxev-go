/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package redisproto

import (
	"bytes"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestParserPartialAndMultiFrame(t *testing.T) {
	parser := NewParser()
	chunks := [][]byte{
		[]byte("+OK\r\n:12"),
		[]byte("3\r\n$3\r\nfoo\r\n"),
	}

	var all []Value
	for i, chunk := range chunks {
		out, err := parser.Feed(chunk)
		if err != nil {
			t.Fatalf("feed %d failed: %v", i, err)
		}
		all = append(all, out...)
	}

	want := []Value{
		{Kind: KindSimpleString, Str: "OK"},
		{Kind: KindInteger, Int: 123},
		{Kind: KindBulkString, Bulk: []byte("foo")},
	}
	if !reflect.DeepEqual(all, want) {
		t.Fatalf("unexpected parsed values: got=%#v want=%#v", all, want)
	}
}

func TestParserArrayAndNull(t *testing.T) {
	parser := NewParser()
	resp := "*4\r\n$3\r\nGET\r\n$3\r\nkey\r\n$-1\r\n:1\r\n"
	out, err := parser.Feed([]byte(resp))
	if err != nil {
		t.Fatalf("feed failed: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(out))
	}

	want := Value{Kind: KindArray, Array: []Value{
		{Kind: KindBulkString, Bulk: []byte("GET")},
		{Kind: KindBulkString, Bulk: []byte("key")},
		{Kind: KindNull},
		{Kind: KindInteger, Int: 1},
	}}
	if !reflect.DeepEqual(out[0], want) {
		t.Fatalf("unexpected frame: got=%#v want=%#v", out[0], want)
	}
}

func TestParserMalformedFrames(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		errLike string
	}{
		{name: "unknown prefix", input: "!oops\r\n", errLike: "unknown RESP2 prefix"},
		{name: "bad integer", input: ":1x\r\n", errLike: "invalid integer"},
		{name: "bad bulk len", input: "$x\r\n", errLike: "invalid bulk string length"},
		{name: "negative bulk len", input: "$-2\r\n", errLike: "negative bulk string length"},
		{name: "negative array len", input: "*-2\r\n", errLike: "negative array length"},
		{name: "broken bulk tail", input: "$3\r\nfooxx", errLike: "bulk string missing CRLF terminator"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser()
			_, err := parser.Feed([]byte(tt.input))
			if err == nil {
				t.Fatalf("expected error")
			}
			if !contains(err, tt.errLike) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEncodeTable(t *testing.T) {
	tests := []struct {
		name string
		in   Value
		out  string
	}{
		{name: "simple", in: Value{Kind: KindSimpleString, Str: "OK"}, out: "+OK\r\n"},
		{name: "error", in: Value{Kind: KindError, Str: "ERR fail"}, out: "-ERR fail\r\n"},
		{name: "integer", in: Value{Kind: KindInteger, Int: -2}, out: ":-2\r\n"},
		{name: "bulk", in: Value{Kind: KindBulkString, Bulk: []byte("foo")}, out: "$3\r\nfoo\r\n"},
		{name: "null", in: Value{Kind: KindNull}, out: "$-1\r\n"},
		{name: "array", in: Value{Kind: KindArray, Array: []Value{{Kind: KindBulkString, Bulk: []byte("PING")}, {Kind: KindBulkString, Bulk: []byte("x")}}}, out: "*2\r\n$4\r\nPING\r\n$1\r\nx\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Encode(tt.in)
			if err != nil {
				t.Fatalf("encode failed: %v", err)
			}
			if string(got) != tt.out {
				t.Fatalf("unexpected encoding: got=%q want=%q", string(got), tt.out)
			}
		})
	}
}

func TestEncodeRejectsInvalidInlineNewline(t *testing.T) {
	_, err := Encode(Value{Kind: KindSimpleString, Str: "bad\r\nvalue"})
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestRoundTripRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	values := make([]Value, 0, 200)

	for i := 0; i < 200; i++ {
		v := randomValue(rng, 0)
		if v.Kind == KindSimpleString || v.Kind == KindError {
			if hasRESPNewline(v.Str) {
				i--
				continue
			}
		}
		values = append(values, v)
	}

	wire := make([]byte, 0, 4096)
	for _, v := range values {
		enc, err := Encode(v)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		wire = append(wire, enc...)
	}

	parser := NewParser()
	got, err := parser.Feed(wire)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !reflect.DeepEqual(got, values) {
		t.Fatalf("round-trip mismatch")
	}

	rest, err := parser.Feed(nil)
	if err != nil {
		t.Fatalf("second feed failed: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("expected empty on second feed, got %d", len(rest))
	}
}

func randomValue(rng *rand.Rand, depth int) Value {
	if depth >= 2 {
		return terminalValue(rng)
	}
	if rng.Intn(100) < 25 {
		return terminalValue(rng)
	}

	n := rng.Intn(4)
	arr := make([]Value, 0, n)
	for i := 0; i < n; i++ {
		arr = append(arr, randomValue(rng, depth+1))
	}
	return Value{Kind: KindArray, Array: arr}
}

func terminalValue(rng *rand.Rand) Value {
	switch rng.Intn(5) {
	case 0:
		return Value{Kind: KindSimpleString, Str: "OK"}
	case 1:
		return Value{Kind: KindError, Str: "ERR sample"}
	case 2:
		return Value{Kind: KindInteger, Int: int64(rng.Intn(2000) - 1000)}
	case 3:
		b := make([]byte, rng.Intn(16))
		for i := range b {
			b[i] = byte('a' + rng.Intn(26))
		}
		return Value{Kind: KindBulkString, Bulk: b}
	default:
		return Value{Kind: KindNull}
	}
}

func contains(err error, want string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), want) || bytes.Contains([]byte(err.Error()), []byte(want))
}
