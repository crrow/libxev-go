/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

import "sync"

func mapCount(m *sync.Map) int {
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// DebugTCPCallbackCount returns the number of active TCP callback registrations.
func DebugTCPCallbackCount() int {
	return mapCount(&tcpCallbackRegistry) +
		mapCount(&tcpAcceptCallbackRegistry) +
		mapCount(&tcpReadCallbackRegistry) +
		mapCount(&tcpWriteCallbackRegistry)
}

// DebugUDPCallbackCount returns the number of active UDP callback registrations.
func DebugUDPCallbackCount() int {
	return mapCount(&udpReadCallbackRegistry) +
		mapCount(&udpWriteCallbackRegistry) +
		mapCount(&udpCallbackRegistry)
}
