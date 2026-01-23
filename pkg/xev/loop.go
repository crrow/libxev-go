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

// Loop is the central event loop that drives all I/O operations.
//
// A Loop manages a set of watchers (timers, sockets, files) and dispatches
// events to their registered callbacks. All operations are non-blocking and
// driven by the underlying OS event notification mechanism (kqueue on macOS,
// io_uring on Linux).
//
// # Thread Safety
//
// A Loop is NOT thread-safe. All operations on a Loop and its associated
// watchers must be performed from the same goroutine. For cross-goroutine
// communication, use channels or the Async watcher (not yet implemented).
//
// # Lifecycle
//
// Create a Loop with [NewLoop] or [NewLoopWithThreadPool], use it to register
// watchers and run events, then call [Loop.Close] when done:
//
//	loop, err := xev.NewLoop()
//	if err != nil {
//	    return err
//	}
//	defer loop.Close()
//
//	// Register watchers...
//	loop.Run()
type Loop struct {
	inner      cxev.Loop
	threadPool cxev.ThreadPool
	hasPool    bool
}

// NewLoop creates a new event loop.
//
// This creates a basic event loop suitable for timers, network I/O, and
// other async operations that don't require blocking syscalls.
//
// For file I/O operations (which may block), use [NewLoopWithThreadPool] instead.
//
// Returns an error if the underlying OS event mechanism cannot be initialized.
func NewLoop() (*Loop, error) {
	l := &Loop{}
	if err := cxev.LoopInit(&l.inner); err != nil {
		return nil, err
	}
	return l, nil
}

// NewLoopWithThreadPool creates an event loop with an integrated thread pool.
//
// The thread pool is required for file I/O operations, which may block and
// cannot be efficiently multiplexed by kernel event mechanisms. The thread
// pool offloads blocking operations to worker threads, delivering completion
// events back to the event loop.
//
// Use this constructor when you need [File] operations. For pure network I/O
// or timers, [NewLoop] is sufficient and has lower overhead.
//
// Example:
//
//	loop, err := xev.NewLoopWithThreadPool()
//	if err != nil {
//	    return err
//	}
//	defer loop.Close()
//
//	file, err := xev.OpenFile("data.txt", os.O_RDONLY, 0)
//	if err != nil {
//	    return err
//	}
//	// Use file with async operations...
func NewLoopWithThreadPool() (*Loop, error) {
	l := &Loop{hasPool: true}

	// Initialize thread pool first
	cxev.ThreadPoolInit(&l.threadPool, nil)

	// Initialize loop with thread pool via options
	opts := &cxev.LoopOptions{
		Entries:    256,
		ThreadPool: &l.threadPool,
	}
	if err := cxev.LoopInitWithOptions(&l.inner, opts); err != nil {
		return nil, err
	}

	return l, nil
}

// Close releases all resources associated with the event loop.
//
// This must be called when the loop is no longer needed to avoid resource
// leaks. If the loop was created with [NewLoopWithThreadPool], this also
// shuts down and cleans up the thread pool.
//
// After Close is called, the Loop must not be used.
func (l *Loop) Close() {
	cxev.LoopDeinit(&l.inner)
	if l.hasPool {
		cxev.ThreadPoolShutdown(&l.threadPool)
		cxev.ThreadPoolDeinit(&l.threadPool)
	}
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
