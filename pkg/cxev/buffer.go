/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

import "unsafe"

func bufferPointer(buf []byte) unsafe.Pointer {
	if len(buf) == 0 {
		return nil
	}
	return unsafe.Pointer(&buf[0])
}
