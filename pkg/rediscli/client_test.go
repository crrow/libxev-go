/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package rediscli

import (
	"bytes"
	"errors"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crrow/libxev-go/pkg/cxev"
	"github.com/crrow/libxev-go/pkg/redismvp"
	"github.com/crrow/libxev-go/pkg/redisproto"
)

func TestRedisCLIBuildCommand(t *testing.T) {
	got := BuildCommand([]string{"SET", "k", "v"})
	want := redisproto.Value{Kind: redisproto.KindArray, Array: []redisproto.Value{
		{Kind: redisproto.KindBulkString, Bulk: []byte("SET")},
		{Kind: redisproto.KindBulkString, Bulk: []byte("k")},
		{Kind: redisproto.KindBulkString, Bulk: []byte("v")},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected command frame: got=%#v want=%#v", got, want)
	}
}

func TestRedisCLIReadResponseProtocolError(t *testing.T) {
	reader := bytes.NewBufferString("!bad\r\n")
	_, err := ReadResponse(reader)
	if err == nil {
		t.Fatalf("expected protocol error")
	}
	if !strings.Contains(err.Error(), "protocol error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRedisCLIRunOneShotAndInteractive(t *testing.T) {
	client := NewClient("fake")
	client.Timeout = time.Second

	var (
		mu        sync.Mutex
		callIndex int
	)
	client.Dial = func(network, addr string) (net.Conn, error) {
		server, cli := net.Pipe()

		mu.Lock()
		idx := callIndex
		callIndex++
		mu.Unlock()

		go func(call int) {
			defer server.Close()
			req := make([]byte, 256)
			n, _ := server.Read(req)
			parser := redisproto.NewParser()
			frames, err := parser.Feed(req[:n])
			if err != nil || len(frames) == 0 {
				return
			}

			var resp redisproto.Value
			switch call {
			case 0:
				resp = redisproto.Value{Kind: redisproto.KindSimpleString, Str: "PONG"}
			case 1:
				resp = redisproto.Value{Kind: redisproto.KindSimpleString, Str: "OK"}
			case 2:
				resp = redisproto.Value{Kind: redisproto.KindBulkString, Bulk: []byte("v")}
			default:
				resp = redisproto.Value{Kind: redisproto.KindError, Str: "ERR value is not an integer or out of range"}
			}
			wire, _ := redisproto.Encode(resp)
			_, _ = server.Write(wire)
		}(idx)

		return cli, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := client.Run([]string{"PING"}, bytes.NewBuffer(nil), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected success exit code, got %d", code)
	}
	if strings.TrimSpace(out.String()) != "PONG" {
		t.Fatalf("unexpected one-shot output: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	in := bytes.NewBufferString("SET k v\nGET k\nINCR k\nquit\n")
	code = client.Run(nil, in, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected interactive success, got %d, err=%q", code, errOut.String())
	}
	stdout := out.String()
	if !strings.Contains(stdout, "redis-cli interactive mode") {
		t.Fatalf("missing interactive banner: %q", stdout)
	}
	if !strings.Contains(stdout, "OK") || !strings.Contains(stdout, "v") {
		t.Fatalf("missing command results: %q", stdout)
	}
	if !strings.Contains(stdout, "(error) ERR value is not an integer or out of range") {
		t.Fatalf("missing server error rendering: %q", stdout)
	}
}

func TestRedisCLIIntegrationWithRedisServerPing(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	srv, err := redismvp.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start server failed: %v", err)
	}
	defer func() { _ = srv.Close() }()

	client := NewClient(srv.Addr())
	client.Timeout = 2 * time.Second

	resp, err := client.Do([]string{"PING"})
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	if resp.Kind != redisproto.KindSimpleString || resp.Str != "PONG" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestRedisCLIRunConnectionErrorMessage(t *testing.T) {
	client := NewClient("127.0.0.1:1")
	client.Dial = func(network, addr string) (net.Conn, error) {
		return nil, errors.New("dial failed")
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := client.Run([]string{"PING"}, bytes.NewBuffer(nil), &out, &errOut)
	if code != 1 {
		t.Fatalf("expected failure exit code, got %d", code)
	}
	if !strings.Contains(errOut.String(), "redis-cli error") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestRedisCLIDoWithNetPipeRoundTrip(t *testing.T) {
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()

	client := NewClient("pipe")
	client.Dial = func(network, addr string) (net.Conn, error) {
		return clientConn, nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		n, _ := server.Read(buf)
		parser := redisproto.NewParser()
		frames, err := parser.Feed(buf[:n])
		if err == nil && len(frames) > 0 {
			resp, _ := redisproto.Encode(redisproto.Value{Kind: redisproto.KindSimpleString, Str: "PONG"})
			_, _ = server.Write(resp)
		}
	}()
	resp, err := client.Do([]string{"PING"})
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	if resp.Kind != redisproto.KindSimpleString || resp.Str != "PONG" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	<-done
}

func TestRedisCLITimeoutError(t *testing.T) {
	client := NewClient("fake")
	client.Timeout = 100 * time.Millisecond
	client.Dial = func(network, addr string) (net.Conn, error) {
		server, cli := net.Pipe()
		go func() {
			defer server.Close()
			_, _ = server.Read(make([]byte, 64))
			// Intentionally never write response to trigger timeout.
			time.Sleep(500 * time.Millisecond)
		}()
		return cli, nil
	}

	_, err := client.Do([]string{"PING"})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(err.Error(), "read response failed") {
		t.Fatalf("unexpected timeout error: %v", err)
	}
}

func TestRedisCLIInteractiveContinuesAfterCommandError(t *testing.T) {
	client := NewClient("fake")
	client.Timeout = time.Second
	client.Dial = func(network, addr string) (net.Conn, error) {
		server, cli := net.Pipe()
		go func() {
			defer server.Close()
			_, _ = server.Read(make([]byte, 128))
			resp, _ := redisproto.Encode(redisproto.Value{
				Kind: redisproto.KindError,
				Str:  "ERR bad",
			})
			_, _ = server.Write(resp)
		}()
		return cli, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	in := bytes.NewBufferString("BAD\\nquit\\n")
	code := client.Run(nil, in, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected success exit code, got %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "(error) ERR bad") {
		t.Fatalf("expected rendered error reply in stdout: %q", out.String())
	}
	if strings.Contains(errOut.String(), "redis-cli error") {
		t.Fatalf("did not expect network/protocol error in stderr: %q", errOut.String())
	}
}
