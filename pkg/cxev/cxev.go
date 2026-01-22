/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

// Package cxev provides low-level Go bindings for libxev, a high-performance
// event loop library written in Zig.
//
// # Architecture Overview
//
// This package uses JupiterRider/ffi (based on libffi) to call C functions
// exported by libxev without cgo. The approach has three key components:
//
//  1. Library Loading: Load the shared library (.dylib/.so/.dll) at runtime
//  2. Function Binding: Prepare FFI call descriptors for each C function
//  3. Callback Mechanism: Create C-callable closures that dispatch to Go code
//
// # Why FFI instead of cgo?
//
// Using FFI over cgo provides several benefits:
//   - Pure Go build: no C compiler required
//   - Cross-compilation friendly
//   - Smaller binary size (no cgo runtime overhead)
//   - Better goroutine integration (no cgo call overhead)
//
// # Memory Layout
//
// libxev uses opaque structs with known maximum sizes. We mirror these as
// fixed-size byte arrays in Go. The actual struct contents are managed by
// libxev internally - Go only needs to allocate the storage.
//
//	Loop       = [512]byte   // xev_loop
//	Completion = [320]byte   // xev_completion (includes extra space for C callback pointer)
//	Watcher    = [256]byte   // xev_timer, xev_async, etc.
//
// # Usage Pattern
//
// Typical usage follows this pattern:
//
//	// 1. Check if library loaded successfully
//	if err := cxev.LoadError(); err != nil {
//	    return err
//	}
//
//	// 2. Initialize structures
//	var loop cxev.Loop
//	if err := cxev.LoopInit(&loop); err != nil {
//	    return err
//	}
//	defer cxev.LoopDeinit(&loop)
//
//	// 3. Register callbacks and run
//	cxev.TimerRunWithCallback(...)
//	cxev.LoopRun(&loop, cxev.RunUntilDone)
//
// For a higher-level, more Go-idiomatic API, see the xev package.
package cxev

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/jupiterrider/ffi"
)

// Opaque struct sizes from libxev's xev.h header (XEV_SIZEOF_* constants).
//
// libxev defines these as maximum sizes for ABI stability. The actual internal
// structs may be smaller, but C/Go code must allocate at least this much space.
// This allows libxev to grow struct internals without breaking binary compatibility.
//
// We define Go types as fixed-size byte arrays matching these sizes. Go manages
// the memory allocation; libxev manages the struct contents via init/deinit calls.
//
// Source: deps/libxev/include/xev.h
const (
	SizeofLoop             = 512 // xev_loop: event loop state
	SizeofCompletion       = 320 // xev_completion: pending operation + callback pointer
	SizeofWatcher          = 256 // xev_watcher: timer/async/file descriptor state
	SizeofThreadPool       = 64  // xev_threadpool: thread pool instance
	SizeofThreadPoolBatch  = 24  // xev_threadpool_batch: grouped task submission
	SizeofThreadPoolTask   = 24  // xev_threadpool_task: single work unit
	SizeofThreadPoolConfig = 64  // xev_threadpool_config: pool configuration
)

// Loop represents the libxev event loop state.
// This is an opaque type - do not access the bytes directly.
// Always pass by pointer to C functions.
type Loop [SizeofLoop]byte

// Completion represents a pending I/O operation or timer.
// Each operation (timer fire, async notification, etc.) requires its own
// Completion instance. The completion tracks the operation state and stores
// the callback to invoke when complete.
//
// IMPORTANT: Completions must remain valid (not garbage collected) until the
// operation completes or is cancelled. Typically, embed completions in a
// longer-lived struct or allocate on the heap.
type Completion [SizeofCompletion]byte

// Watcher is a generic watcher type that can hold timer, async, or other
// watcher state. Like Loop and Completion, this is opaque storage.
type Watcher [SizeofWatcher]byte

// ThreadPool is the libxev thread pool for offloading blocking work.
type ThreadPool [SizeofThreadPool]byte

// ThreadPoolBatch groups multiple tasks for efficient scheduling.
type ThreadPoolBatch [SizeofThreadPoolBatch]byte

// ThreadPoolTask represents a unit of work for the thread pool.
type ThreadPoolTask [SizeofThreadPoolTask]byte

// ThreadPoolConfig configures thread pool parameters (stack size, max threads).
type ThreadPoolConfig [SizeofThreadPoolConfig]byte

// RunMode controls how the event loop processes events.
type RunMode int32

const (
	// RunNoWait polls for ready events without blocking.
	// Returns immediately even if no events are ready.
	// Use for non-blocking polling in a game loop or similar.
	RunNoWait RunMode = 0

	// RunOnce blocks until at least one event is ready, then processes all
	// ready events and returns. Useful for integrating with other event sources.
	RunOnce RunMode = 1

	// RunUntilDone blocks and processes events until there are no more
	// registered watchers. This is the typical mode for a main event loop.
	RunUntilDone RunMode = 2
)

// CbAction is the return value from callbacks, controlling whether the
// watcher should continue or stop.
type CbAction int32

const (
	// Disarm stops the watcher after this callback returns.
	// For timers, this means the timer will not fire again.
	Disarm CbAction = 0

	// Rearm keeps the watcher active after this callback returns.
	// For timers, this allows periodic firing (timer resets to same interval).
	Rearm CbAction = 1
)

// CompletionState represents the current state of a completion.
type CompletionState int32

const (
	// CompletionDead means the completion is not active.
	CompletionDead CompletionState = 0

	// CompletionActive means the completion has a pending operation.
	CompletionActive CompletionState = 1
)

// Package-level state for library loading.
// The library is loaded once on package init and the result is cached.
var (
	lib     ffi.Lib // Handle to the loaded libxev shared library
	libExt  ffi.Lib // Handle to the extended API library (TCP, etc.)
	loadErr error   // Error from loading, nil if successful
	once    sync.Once
)

// init loads the libxev shared library when the package is imported.
// The library path is determined by:
//  1. LIBXEV_PATH environment variable (if set)
//  2. Same directory as the executable
//  3. System library search path (LD_LIBRARY_PATH, etc.)
//
// After loading, all FFI function descriptors are prepared. Any error
// is stored in loadErr and can be retrieved via LoadError().
func init() {
	once.Do(func() {
		lib, loadErr = ffi.Load(libPath())
		if loadErr != nil {
			return
		}
		loadErr = registerFunctions()
		if loadErr != nil {
			return
		}

		// Load extended library (TCP, File support) if available
		extPath := libExtPath()
		if extPath != "" {
			libExt, loadErr = ffi.Load(extPath)
			if loadErr != nil {
				return
			}
			loadErr = registerTCPFunctions()
			if loadErr != nil {
				return
			}
			loadErr = registerFileFunctions()
			if loadErr != nil {
				return
			}
			loadErr = registerUDPFunctions()
			if loadErr != nil {
				return
			}
			loadErr = registerExtendedFunctions()
		}
	})
}

// libPath determines the path to the libxev shared library.
// Priority:
//  1. LIBXEV_PATH environment variable
//  2. Library file adjacent to executable
//  3. Default library name (system will search LD_LIBRARY_PATH, etc.)
func libPath() string {
	// Allow explicit override via environment variable
	if p := os.Getenv("LIBXEV_PATH"); p != "" {
		return p
	}

	// Check for library adjacent to executable
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, libName())
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Fall back to system library search
	return libName()
}

// libName returns the platform-specific library filename.
func libName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libxev.dylib"
	case "linux":
		return "libxev.so"
	case "windows":
		return "xev.dll"
	default:
		return "libxev.so"
	}
}

// libExtPath returns the path to the extended API library (TCP support).
// Returns empty string if not available.
func libExtPath() string {
	if p := os.Getenv("LIBXEV_EXT_PATH"); p != "" {
		return p
	}

	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, libExtName())
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

func libExtName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libxev_extended.dylib"
	case "linux":
		return "libxev_extended.so"
	case "windows":
		return "xev_extended.dll"
	default:
		return "libxev_extended.so"
	}
}

// LoadError returns any error that occurred during library loading.
// Returns nil if the library was loaded successfully.
// Call this before using any other functions in this package.
func LoadError() error {
	return loadErr
}

// GetLib returns the loaded library handle for advanced use cases.
// Most users should use the provided wrapper functions instead.
func GetLib() ffi.Lib {
	return lib
}
