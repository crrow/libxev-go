/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/crrow/libxev-go/pkg/cxev"
)

func TestEmptyBufferReturnsError(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	udpConn, err := NewUDPConn()
	if err != nil {
		t.Fatalf("NewUDPConn failed: %v", err)
	}
	defer udpConn.Cleanup()

	tcpConn, err := Dial("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	udpAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}

	checkEmptyErr := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrEmptyBuffer) {
			t.Fatalf("%s: expected ErrEmptyBuffer, got %v", name, err)
		}
	}

	checkEmptyErr("tcp read", tcpConn.ReadFunc(loop, []byte{}, func(conn *TCPConn, data []byte, err error) Action {
		return Stop
	}))
	checkEmptyErr("tcp write", tcpConn.WriteFunc(loop, []byte{}, func(conn *TCPConn, bytesWritten int, err error) Action {
		return Stop
	}))

	checkEmptyErr("udp read", udpConn.ReadFromFunc(loop, []byte{}, func(conn *UDPConn, data []byte, remoteAddr *net.UDPAddr, err error) Action {
		return Stop
	}))
	checkEmptyErr("udp write to", udpConn.WriteToFunc(loop, []byte{}, "127.0.0.1:12345", func(conn *UDPConn, bytesWritten int, err error) Action {
		return Stop
	}))
	checkEmptyErr("udp write to addr", udpConn.WriteToAddrFunc(loop, []byte{}, udpAddr, func(conn *UDPConn, bytesWritten int, err error) Action {
		return Stop
	}))
}

func TestFileEmptyBufferReturnsError(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	loop, err := NewLoopWithThreadPool()
	if err != nil {
		t.Fatalf("NewLoopWithThreadPool failed: %v", err)
	}
	defer loop.Close()

	path := filepath.Join(t.TempDir(), "empty-buffer.txt")
	file, err := OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer file.Cleanup()

	checkEmptyErr := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrEmptyBuffer) {
			t.Fatalf("%s: expected ErrEmptyBuffer, got %v", name, err)
		}
	}

	checkEmptyErr("file read", file.ReadFunc(loop, []byte{}, func(file *File, data []byte, err error) Action {
		return Stop
	}))
	checkEmptyErr("file write", file.WriteFunc(loop, []byte{}, func(file *File, bytesWritten int, err error) Action {
		return Stop
	}))
	checkEmptyErr("file pread", file.PReadFunc(loop, []byte{}, 0, func(file *File, data []byte, err error) Action {
		return Stop
	}))
	checkEmptyErr("file pwrite", file.PWriteFunc(loop, []byte{}, 0, func(file *File, bytesWritten int, err error) Action {
		return Stop
	}))
}
