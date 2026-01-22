/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"net"
	"testing"
)

func TestUDPBind(t *testing.T) {
	conn, err := ListenUDP("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenUDP failed: %v", err)
	}
	defer conn.Cleanup()

	_, port := conn.LocalAddr()
	if port == 0 {
		t.Error("expected non-zero port")
	}
	t.Logf("UDP socket bound to port %d", port)
}

func TestUDPEcho(t *testing.T) {
	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	server, err := ListenUDP("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenUDP for server failed: %v", err)
	}
	defer server.Cleanup()

	_, serverPort := server.LocalAddr()
	serverAddr := "127.0.0.1:" + portToString(serverPort)
	t.Logf("Server listening on %s", serverAddr)

	client, err := NewUDPConn()
	if err != nil {
		t.Fatalf("NewUDPConn for client failed: %v", err)
	}
	defer client.Cleanup()

	testMessage := []byte("Hello UDP!")
	var receivedData []byte
	var receivedFrom *net.UDPAddr
	serverDone := false
	clientDone := false

	serverBuf := make([]byte, 1024)
	server.ReadFromFunc(loop, serverBuf, func(conn *UDPConn, data []byte, remoteAddr *net.UDPAddr, err error) Action {
		if err != nil {
			t.Errorf("Server read error: %v", err)
			return Stop
		}

		receivedData = make([]byte, len(data))
		copy(receivedData, data)
		receivedFrom = remoteAddr

		t.Logf("Server received %d bytes from %v: %s", len(data), remoteAddr, string(data))

		conn.WriteToAddrFunc(loop, data, remoteAddr, func(conn *UDPConn, bytesWritten int, err error) Action {
			if err != nil {
				t.Errorf("Server write error: %v", err)
			}
			t.Logf("Server sent %d bytes back", bytesWritten)
			serverDone = true
			return Stop
		})

		return Stop
	})

	var echoData []byte
	clientBuf := make([]byte, 1024)

	sendAndReceive := func() {
		client.WriteToFunc(loop, testMessage, serverAddr, func(conn *UDPConn, bytesWritten int, err error) Action {
			if err != nil {
				t.Errorf("Client write error: %v", err)
				return Stop
			}
			t.Logf("Client sent %d bytes", bytesWritten)

			client.ReadFromFunc(loop, clientBuf, func(conn *UDPConn, data []byte, remoteAddr *net.UDPAddr, err error) Action {
				if err != nil {
					t.Errorf("Client read error: %v", err)
					return Stop
				}
				echoData = make([]byte, len(data))
				copy(echoData, data)
				t.Logf("Client received echo: %s", string(data))
				clientDone = true
				return Stop
			})

			return Stop
		})
	}

	sendAndReceive()

	loop.Run()

	if !serverDone {
		t.Error("Server did not complete")
	}
	if !clientDone {
		t.Error("Client did not complete")
	}

	if string(receivedData) != string(testMessage) {
		t.Errorf("Server received %q, expected %q", string(receivedData), string(testMessage))
	}

	if receivedFrom == nil {
		t.Error("Server did not receive sender address")
	}

	if string(echoData) != string(testMessage) {
		t.Errorf("Client received echo %q, expected %q", string(echoData), string(testMessage))
	}
}

func TestNewUDPConn(t *testing.T) {
	conn, err := NewUDPConn()
	if err != nil {
		t.Fatalf("NewUDPConn failed: %v", err)
	}
	defer conn.Cleanup()

	err = conn.Bind("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Bind failed: %v", err)
	}

	_, port := conn.LocalAddr()
	if port == 0 {
		t.Error("expected non-zero port after bind")
	}
	t.Logf("UDP socket bound to port %d", port)
}

func portToString(port uint16) string {
	result := make([]byte, 0, 5)
	if port == 0 {
		return "0"
	}
	for port > 0 {
		result = append([]byte{byte('0' + port%10)}, result...)
		port /= 10
	}
	return string(result)
}
