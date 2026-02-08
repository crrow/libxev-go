/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

import "testing"

func TestRegisterReadCallbacksWithEmptyBuffer(t *testing.T) {
	tcpID := RegisterTCPReadCallback(func(loop *Loop, c *TCPCompletion, buf []byte, bytesRead int32, err int32, userdata uintptr) CbAction {
		return Disarm
	}, []byte{})
	UnregisterTCPCallback(tcpID)

	fileID := RegisterFileReadCallback(func(loop *Loop, c *FileCompletion, buf []byte, bytesRead int32, err int32, userdata uintptr) CbAction {
		return Disarm
	}, []byte{})
	UnregisterFileCallback(fileID)

	udpID := RegisterUDPReadCallback(func(loop *Loop, c *UDPCompletion, remoteAddr *Sockaddr, buf []byte, bytesRead int32, err int32, userdata uintptr) CbAction {
		return Disarm
	}, []byte{})
	UnregisterUDPCallback(udpID)
}
