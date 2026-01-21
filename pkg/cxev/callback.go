/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

// This file implements the callback mechanism that allows Go functions to be
// called from C code (libxev).
//
// # The Problem
//
// libxev is a C library that uses callbacks for async notifications. When a
// timer fires or I/O completes, libxev calls a function pointer we provide.
// But Go functions cannot be directly called from C - we need a trampoline.
//
// # The Solution: libffi Closures
//
// libffi provides "closures" - dynamically generated machine code that:
//  1. Has a stable C-callable address (can be passed to libxev)
//  2. When called, invokes our Go trampoline function
//  3. Marshals C arguments to Go types
//
// # Architecture
//
//	┌─────────────┐     callback ptr     ┌──────────────┐
//	│   libxev    │ ──────────────────▶ │ ffi.Closure  │
//	│  (C code)   │                      │ (asm thunk)  │
//	└─────────────┘                      └──────┬───────┘
//	                                            │
//	                                            ▼
//	                                    ┌───────────────────┐
//	                                    │ timerTrampoline   │
//	                                    │ (Go function)     │
//	                                    └───────┬───────────┘
//	                                            │ lookup userdata
//	                                            ▼
//	                                    ┌───────────────────┐
//	                                    │ callbackRegistry  │
//	                                    │ (sync.Map)        │
//	                                    └───────┬───────────┘
//	                                            │
//	                                            ▼
//	                                    ┌───────────────────┐
//	                                    │ User's Go callback│
//	                                    └───────────────────┘
//
// # Key Components
//
//  1. ffi.Closure: Allocated via ffi.ClosureAlloc, holds the generated thunk
//  2. timerClosureCode: The actual executable address passed to libxev
//  3. timerCif: CIF describing the callback's C signature
//  4. callbackRegistry: Maps userdata IDs to Go callback functions
//
// # Thread Safety
//
// The callback registry uses sync.Map for concurrent access. Callbacks may be
// invoked from any thread (though libxev typically uses a single thread).
// The closure itself is allocated once and never freed (lives for program lifetime).
//
// # Why userdata for dispatch?
//
// libxev passes a userdata pointer through to callbacks. We use this to store
// a unique ID that maps to the actual Go callback. This allows multiple timers
// to use the same closure code but dispatch to different Go functions.

package cxev

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/jupiterrider/ffi"
)

// TimerCallback is the Go function signature for timer callbacks.
// Parameters:
//   - loop: The event loop (for scheduling more operations)
//   - c: The completion that triggered this callback
//   - result: 0 on success, error code on failure
//   - userdata: The ID returned by RegisterCallback
//
// Return CbAction to control timer behavior:
//   - Disarm: Stop the timer
//   - Rearm: Repeat with the same interval
type TimerCallback func(loop *Loop, c *Completion, result int32, userdata uintptr) CbAction

// Callback registry state.
// We use a monotonic counter for IDs to avoid ABA problems.
var (
	callbackRegistry sync.Map // map[uintptr]TimerCallback
	callbackCounter  uint64   // atomic counter for generating unique IDs
)

// Closure state - initialized once, lives forever.
// We use a single closure for all timer callbacks, dispatching via userdata.
var (
	timerCallbackPtr uintptr        // C-callable address to pass to libxev
	closureInit      sync.Once      // ensures one-time initialization
	timerClosure     *ffi.Closure   // the closure struct (must stay allocated)
	timerClosureCode unsafe.Pointer // executable code address
	timerCif         ffi.Cif        // Call Interface for the callback signature
)

// initTimerClosure creates the libffi closure for timer callbacks.
// This is called lazily on first use and only once.
//
// # Closure Creation Steps
//
//  1. ClosureAlloc: Allocate memory for closure + get executable code pointer
//  2. PrepCif: Define the C function signature (return type + arg types)
//  3. NewCallback: Wrap our Go trampoline as a libffi callback
//  4. PrepClosureLoc: Wire everything together
//
// After this, timerClosureCode can be passed to any C function expecting a
// function pointer with the matching signature.
func initTimerClosure() {
	closureInit.Do(func() {
		// Step 1: Allocate closure memory.
		// timerClosureCode receives a pointer to executable memory.
		timerClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &timerClosureCode)

		// Step 2: Prepare CIF describing the callback signature.
		// C signature: int32_t callback(void* loop, void* completion, int32_t result, void* userdata)
		if status := ffi.PrepCif(&timerCif, ffi.DefaultAbi, 4,
			&ffi.TypeSint32,  // return type: CbAction (int32)
			&ffi.TypePointer, // arg 0: xev_loop*
			&ffi.TypePointer, // arg 1: xev_completion*
			&ffi.TypeSint32,  // arg 2: result code
			&ffi.TypePointer, // arg 3: userdata
		); status != ffi.OK {
			panic("failed to prepare timer callback CIF")
		}

		// Step 3: Create Go callback wrapper.
		// ffi.NewCallback returns a C function pointer that calls our Go function.
		goCallback := ffi.NewCallback(timerTrampolineClosure)

		// Step 4: Prepare the closure.
		// This wires: timerClosureCode -> timerCif + goCallback
		if status := ffi.PrepClosureLoc(timerClosure, &timerCif, goCallback, nil, timerClosureCode); status != ffi.OK {
			panic("failed to prepare timer closure")
		}

		timerCallbackPtr = uintptr(timerClosureCode)
	})
}

// timerTrampolineClosure is invoked by libffi when C code calls our closure.
//
// # Parameter Layout
//
// libffi passes arguments as an array of pointers. Each pointer points to
// the actual argument value in memory. We must dereference carefully:
//
//	args[0] -> pointer to (void* loop)      -> dereference to get loop pointer
//	args[1] -> pointer to (void* completion)-> dereference to get completion pointer
//	args[2] -> pointer to (int32_t result)  -> dereference to get int32 value
//	args[3] -> pointer to (void* userdata)  -> dereference to get uintptr value
//
// The return value is written to 'ret' as int32.
func timerTrampolineClosure(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	// Convert args pointer to slice for indexing
	arguments := unsafe.Slice(args, 4)

	// Extract each argument by dereferencing the pointer-to-pointer
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	result := *(*int32)(arguments[2])
	userdata := *(*uintptr)(arguments[3])

	// Default to Disarm if callback not found (defensive)
	action := int32(Disarm)

	// Look up and invoke the registered Go callback
	if cb, ok := callbackRegistry.Load(userdata); ok {
		action = int32(cb.(TimerCallback)(
			(*Loop)(loop),
			(*Completion)(completion),
			result,
			userdata,
		))
	}

	// Write return value
	*(*int32)(ret) = action
	return 0
}

// RegisterCallback registers a Go callback and returns its unique ID.
// Pass this ID as userdata when calling TimerRun.
// The callback will be invoked when the timer fires.
func RegisterCallback(cb TimerCallback) uintptr {
	id := uintptr(atomic.AddUint64(&callbackCounter, 1))
	callbackRegistry.Store(id, cb)
	return id
}

// UnregisterCallback removes a callback from the registry.
// Call this after the timer is done to avoid memory leaks.
func UnregisterCallback(id uintptr) {
	callbackRegistry.Delete(id)
}

// GetTimerCallbackPtr returns the C function pointer for timer callbacks.
// This is the address to pass to TimerRun's cb parameter.
func GetTimerCallbackPtr() uintptr {
	initTimerClosure()
	return timerCallbackPtr
}

// TimerRunWithCallback is a convenience function that registers the callback
// and starts the timer in one call.
// Returns the callback ID (needed for UnregisterCallback).
func TimerRunWithCallback(w *Watcher, loop *Loop, c *Completion, delayMs uint64, cb TimerCallback) uintptr {
	initTimerClosure()
	id := RegisterCallback(cb)
	TimerRun(w, loop, c, delayMs, id, timerCallbackPtr)
	return id
}
