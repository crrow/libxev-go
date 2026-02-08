/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"testing"

	"github.com/crrow/libxev-go/pkg/cxev"
)

func TestTCPListenerBind(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	listener, err := Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	_, port := listener.Addr()
	if port == 0 {
		t.Error("expected non-zero port")
	}
	t.Logf("listening on port %d", port)
}

func TestTCPEchoServer(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	listener, err := Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	_, port := listener.Addr()

	serverDone := false
	clientDone := false
	echoedData := ""

	err = listener.AcceptFunc(loop, func(l *TCPListener, conn *TCPConn, err error) Action {
		if err != nil {
			t.Errorf("accept error: %v", err)
			return Stop
		}

		buf := make([]byte, 1024)
		conn.ReadFunc(loop, buf, func(c *TCPConn, data []byte, err error) Action {
			if err != nil {
				return Stop
			}

			c.WriteFunc(loop, data, func(c *TCPConn, written int, err error) Action {
				serverDone = true
				c.CloseFunc(loop, nil)
				return Stop
			})
			return Stop
		})

		return Stop
	})
	if err != nil {
		t.Fatalf("Accept failed: %v", err)
	}

	client, err := Dial("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	err = client.Connect(loop, "127.0.0.1:"+itoa(int(port)), func(c *TCPConn, err error) Action {
		if err != nil {
			t.Errorf("connect error: %v", err)
			return Stop
		}

		c.WriteFunc(loop, []byte("hello"), func(c *TCPConn, written int, err error) Action {
			if err != nil {
				t.Errorf("write error: %v", err)
				return Stop
			}

			buf := make([]byte, 1024)
			c.ReadFunc(loop, buf, func(c *TCPConn, data []byte, err error) Action {
				echoedData = string(data)
				clientDone = true
				c.CloseFunc(loop, nil)
				return Stop
			})
			return Stop
		})
		return Stop
	})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	for i := 0; i < 1000 && (!serverDone || !clientDone); i++ {
		loop.RunOnce()
	}

	for i := 0; i < 50; i++ {
		loop.Poll()
	}

	if !serverDone {
		t.Error("server did not complete")
	}
	if !clientDone {
		t.Error("client did not complete")
	}
	if echoedData != "hello" {
		t.Errorf("expected 'hello', got '%s'", echoedData)
	}
	if n := cxev.DebugTCPCallbackCount(); n != 0 {
		t.Fatalf("expected no TCP callback leaks, found %d active registrations", n)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
