/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package redismvp

import (
	"bufio"
	"fmt"
	"net"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/crrow/libxev-go/pkg/cxev"
	"github.com/crrow/libxev-go/pkg/redisproto"
)

func TestRedisServerCommandSemantics(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	srv, err := Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = srv.Close() }()

	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	mustResponse(t, conn, []string{"PING"}, redisproto.Value{Kind: redisproto.KindSimpleString, Str: "PONG"})
	mustResponse(t, conn, []string{"PING", "hello"}, redisproto.Value{Kind: redisproto.KindBulkString, Bulk: []byte("hello")})
	mustResponse(t, conn, []string{"ECHO", "abc"}, redisproto.Value{Kind: redisproto.KindBulkString, Bulk: []byte("abc")})
	mustResponse(t, conn, []string{"SET", "k", "v"}, redisproto.Value{Kind: redisproto.KindSimpleString, Str: "OK"})
	mustResponse(t, conn, []string{"GET", "k"}, redisproto.Value{Kind: redisproto.KindBulkString, Bulk: []byte("v")})
	mustResponse(t, conn, []string{"GET", "missing"}, redisproto.Value{Kind: redisproto.KindNull})
	mustResponse(t, conn, []string{"DEL", "k", "missing"}, redisproto.Value{Kind: redisproto.KindInteger, Int: 1})
	mustResponse(t, conn, []string{"INCR", "counter"}, redisproto.Value{Kind: redisproto.KindInteger, Int: 1})
	mustResponse(t, conn, []string{"INCR", "counter"}, redisproto.Value{Kind: redisproto.KindInteger, Int: 2})
	mustResponse(t, conn, []string{"SET", "notint", "abc"}, redisproto.Value{Kind: redisproto.KindSimpleString, Str: "OK"})

	resp := sendCommand(t, conn, []string{"INCR", "notint"})
	if resp.Kind != redisproto.KindError {
		t.Fatalf("expected error, got %#v", resp)
	}
	if resp.Str != "ERR value is not an integer or out of range" {
		t.Fatalf("unexpected error: %q", resp.Str)
	}
}

func TestRedisServerConcurrentClients(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	srv, err := Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = srv.Close() }()

	const clients = 16
	results := make([]int64, 0, clients)
	mu := sync.Mutex{}
	wg := sync.WaitGroup{}
	wg.Add(clients)

	for i := 0; i < clients; i++ {
		go func() {
			defer wg.Done()

			conn, dialErr := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
			if dialErr != nil {
				t.Errorf("dial failed: %v", dialErr)
				return
			}
			defer conn.Close()

			resp := sendCommand(t, conn, []string{"INCR", "shared_counter"})
			if resp.Kind != redisproto.KindInteger {
				t.Errorf("expected integer, got %#v", resp)
				return
			}

			mu.Lock()
			results = append(results, resp.Int)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(results) != clients {
		t.Fatalf("expected %d results, got %d", clients, len(results))
	}
	sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })
	for i := 0; i < clients; i++ {
		want := int64(i + 1)
		if results[i] != want {
			t.Fatalf("unexpected sequence: got=%v", results)
		}
	}
}

func TestRedisServerCloseWithActiveClients(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	srv, err := Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	conns := make([]net.Conn, 0, 24)
	for i := 0; i < 24; i++ {
		conn, dialErr := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
		if dialErr != nil {
			t.Fatalf("dial failed: %v", dialErr)
		}
		conns = append(conns, conn)
	}

	if closeErr := srv.Close(); closeErr != nil {
		t.Fatalf("server close failed: %v", closeErr)
	}

	for _, conn := range conns {
		_ = conn.Close()
	}
}

func TestRedisServerProtocolErrorsDeterministic(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	srv, err := Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = srv.Close() }()

	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	_, _ = conn.Write([]byte("+HELLO\r\n"))
	resp := readOneValue(t, conn)
	if resp.Kind != redisproto.KindError {
		t.Fatalf("expected protocol error, got %#v", resp)
	}
	if resp.Str != "ERR Protocol error: command must be array" {
		t.Fatalf("unexpected error: %q", resp.Str)
	}

	resp = sendCommand(t, conn, []string{"PING", "a", "b"})
	if resp.Kind != redisproto.KindError {
		t.Fatalf("expected arity error, got %#v", resp)
	}
	if resp.Str != "ERR wrong number of arguments for 'ping' command" {
		t.Fatalf("unexpected ping error: %q", resp.Str)
	}

	resp = sendCommand(t, conn, []string{"NOEXIST"})
	if resp.Kind != redisproto.KindError {
		t.Fatalf("expected unknown command error, got %#v", resp)
	}
	if resp.Str != "ERR unknown command 'noexist'" {
		t.Fatalf("unexpected unknown command error: %q", resp.Str)
	}
}

func mustResponse(t *testing.T, conn net.Conn, cmd []string, want redisproto.Value) {
	t.Helper()
	got := sendCommand(t, conn, cmd)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command %v got=%#v want=%#v", cmd, got, want)
	}
}

func sendCommand(t *testing.T, conn net.Conn, args []string) redisproto.Value {
	t.Helper()

	array := redisproto.Value{Kind: redisproto.KindArray, Array: make([]redisproto.Value, 0, len(args))}
	for _, arg := range args {
		array.Array = append(array.Array, redisproto.Value{Kind: redisproto.KindBulkString, Bulk: []byte(arg)})
	}
	wire, err := redisproto.Encode(array)
	if err != nil {
		t.Fatalf("encode command failed: %v", err)
	}
	if _, err = conn.Write(wire); err != nil {
		t.Fatalf("write command failed: %v", err)
	}
	return readOneValue(t, conn)
}

func readOneValue(t *testing.T, conn net.Conn) redisproto.Value {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	parser := redisproto.NewParser()
	r := bufio.NewReader(conn)
	for {
		buf := make([]byte, 4096)
		n, err := r.Read(buf)
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		frames, parseErr := parser.Feed(buf[:n])
		if parseErr != nil {
			t.Fatalf("parse response failed: %v", parseErr)
		}
		if len(frames) > 0 {
			return frames[0]
		}
	}
}

func TestDecodeCommand(t *testing.T) {
	name, args, err := decodeCommand(redisproto.Value{Kind: redisproto.KindArray, Array: []redisproto.Value{
		{Kind: redisproto.KindBulkString, Bulk: []byte("set")},
		{Kind: redisproto.KindBulkString, Bulk: []byte("k")},
		{Kind: redisproto.KindBulkString, Bulk: []byte("v")},
	}})
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if name != "SET" {
		t.Fatalf("unexpected name: %s", name)
	}
	if fmt.Sprint(args) != "[k v]" {
		t.Fatalf("unexpected args: %v", args)
	}
}
