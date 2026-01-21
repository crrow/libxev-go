/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

// Package xev provides a high-level, Go-idiomatic API for libxev event loop.
//
// This package wraps the low-level cxev bindings with:
//   - Go-style error handling (error returns instead of error codes)
//   - time.Duration instead of raw milliseconds
//   - Handler interfaces and functional callbacks
//   - Automatic resource management
//
// # Quick Start
//
//	loop, _ := xev.NewLoop()
//	defer loop.Close()
//
//	timer, _ := xev.NewTimer()
//	defer timer.Close()
//
//	timer.RunFunc(loop, 100*time.Millisecond, func(t *xev.Timer, err error) xev.Action {
//	    fmt.Println("Timer fired!")
//	    return xev.Stop
//	})
//
//	loop.Run()
//
// # Architecture
//
// The xev package is layered on top of cxev:
//
//	┌─────────────────────────────────────┐
//	│  Your Application                   │
//	├─────────────────────────────────────┤
//	│  xev (high-level Go API)            │  <- This package
//	├─────────────────────────────────────┤
//	│  cxev (low-level FFI bindings)      │
//	├─────────────────────────────────────┤
//	│  libffi (C calling convention)      │
//	├─────────────────────────────────────┤
//	│  libxev (Zig event loop library)    │
//	└─────────────────────────────────────┘
package xev

import (
	"time"

	"github.com/crrow/libxev-go/pkg/cxev"
)

// Loop wraps the libxev event loop with a Go-friendly API.
type Loop struct {
	inner cxev.Loop
}

// NewLoop creates and initializes a new event loop.
func NewLoop() (*Loop, error) {
	l := &Loop{}
	if err := cxev.LoopInit(&l.inner); err != nil {
		return nil, err
	}
	return l, nil
}

// Close releases resources associated with the loop.
// Must be called when done with the loop.
func (l *Loop) Close() {
	cxev.LoopDeinit(&l.inner)
}

// Run processes events until all watchers are removed.
// This is the main entry point for running the event loop.
func (l *Loop) Run() error {
	return cxev.LoopRun(&l.inner, cxev.RunUntilDone)
}

// RunOnce blocks until at least one event is ready, processes it, then returns.
// Useful for integrating with other event sources or custom loop logic.
func (l *Loop) RunOnce() error {
	return cxev.LoopRun(&l.inner, cxev.RunOnce)
}

// Poll checks for ready events without blocking.
// Processes any events that are immediately ready and returns.
func (l *Loop) Poll() error {
	return cxev.LoopRun(&l.inner, cxev.RunNoWait)
}

// Now returns the loop's cached timestamp.
// This is efficient (no syscall) but may be slightly stale.
func (l *Loop) Now() time.Duration {
	ms := cxev.LoopNow(&l.inner)
	return time.Duration(ms) * time.Millisecond
}

// Inner returns the underlying cxev.Loop for advanced use cases.
// Most users should not need this.
func (l *Loop) Inner() *cxev.Loop {
	return &l.inner
}
