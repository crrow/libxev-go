/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

import (
	"errors"
	"unsafe"

	"github.com/jupiterrider/ffi"
)

// FFI function descriptors for loop operations.
// These are prepared once during init() and reused for all calls.
//
// The ffi.Fun type encapsulates:
//   - Symbol address in the loaded library
//   - Call Interface (CIF) describing the function signature
//   - Type information for marshaling arguments and return values
var (
	fnLoopInit            ffi.Fun
	fnLoopInitWithOptions ffi.Fun
	fnLoopDeinit          ffi.Fun
	fnLoopRun             ffi.Fun
	fnLoopNow             ffi.Fun
	fnLoopUpdateNow       ffi.Fun
)

// registerFunctions prepares all FFI function descriptors.
//
// # How lib.Prep Works
//
// lib.Prep(name, retType, argTypes...) does the following:
//  1. Looks up the symbol 'name' in the loaded library
//  2. Creates a CIF (Call Interface) describing the function signature
//  3. Returns a ffi.Fun that can be used for calls
//
// The type descriptors (ffi.TypeSint32, ffi.TypePointer, etc.) tell libffi
// how to marshal data between Go and C calling conventions.
//
// # Type Mapping
//
//	C type          Go type           ffi.Type
//	-------         -------           --------
//	int             int32             TypeSint32
//	int64_t         int64             TypeSint64
//	uint64_t        uint64            TypeUint64
//	void*           unsafe.Pointer    TypePointer
//	void            (no return)       TypeVoid
func registerFunctions() error {
	var err error

	// int xev_loop_init(xev_loop* loop)
	// Initializes an event loop. Returns 0 on success, error code on failure.
	fnLoopInit, err = lib.Prep("xev_loop_init", &ffi.TypeSint32, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_loop_deinit(xev_loop* loop)
	// Releases resources associated with the loop.
	fnLoopDeinit, err = lib.Prep("xev_loop_deinit", &ffi.TypeVoid, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// int xev_loop_run(xev_loop* loop, int mode)
	// Runs the event loop. Mode controls blocking behavior (see RunMode).
	fnLoopRun, err = lib.Prep("xev_loop_run", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// int64_t xev_loop_now(xev_loop* loop)
	// Returns the cached timestamp (milliseconds since unspecified epoch).
	fnLoopNow, err = lib.Prep("xev_loop_now", &ffi.TypeSint64, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_loop_update_now(xev_loop* loop)
	// Updates the cached timestamp to current time.
	fnLoopUpdateNow, err = lib.Prep("xev_loop_update_now", &ffi.TypeVoid, &ffi.TypePointer)
	if err != nil {
		return err
	}

	return registerTimerFunctions()
}

func registerExtendedFunctions() error {
	var err error

	// int xev_loop_init_with_options(xev_loop* loop, xev_options* options)
	// Initialize loop with options including thread pool support
	if libExt.Addr != 0 {
		fnLoopInitWithOptions, err = libExt.Prep("xev_loop_init_with_options", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypePointer)
		if err != nil {
			return err
		}
	}

	return registerThreadPoolFunctions()
}

// LoopInit initializes an event loop.
//
// # FFI Call Pattern
//
// All FFI calls follow this pattern:
//
//  1. Declare a variable for the return value (ffi.Arg or specific type)
//  2. Create local variables for each argument with correct types
//  3. Call fn.Call(&ret, &arg1, &arg2, ...)
//
// Arguments are passed by pointer because libffi needs to read them at
// known memory locations. The pointer types (unsafe.Pointer, uintptr) are
// passed as-is since they're already pointer-sized values.
//
// Example breakdown:
//
//	var ret ffi.Arg          // Will hold int32 return value
//	ptr := unsafe.Pointer(loop)  // Convert *Loop to unsafe.Pointer
//	fnLoopInit.Call(&ret, &ptr)  // &ptr because libffi reads from this address
func LoopInit(loop *Loop) error {
	if loadErr != nil {
		return loadErr
	}
	var ret ffi.Arg
	ptr := unsafe.Pointer(loop)
	fnLoopInit.Call(&ret, &ptr)
	if int32(ret) != 0 {
		return errors.New("xev_loop_init failed")
	}
	return nil
}

// LoopInitWithOptions initializes a loop with custom options.
// This allows setting a thread pool during initialization, which is required
// for the new libxev API (thread_pool can no longer be set after init).
func LoopInitWithOptions(loop *Loop, options *LoopOptions) error {
	if loadErr != nil {
		return loadErr
	}
	if fnLoopInitWithOptions.Addr == 0 {
		return errors.New("xev_loop_init_with_options not available (extended library not loaded)")
	}

	var ret ffi.Arg
	loopPtr := unsafe.Pointer(loop)
	optsPtr := unsafe.Pointer(options)
	fnLoopInitWithOptions.Call(&ret, &loopPtr, &optsPtr)
	if int32(ret) != 0 {
		return errors.New("xev_loop_init_with_options failed")
	}
	return nil
}

// LoopDeinit releases resources for an event loop.
// Must be called when done with the loop to avoid resource leaks.
func LoopDeinit(loop *Loop) {
	ptr := unsafe.Pointer(loop)
	fnLoopDeinit.Call(nil, &ptr)
}

// LoopRun runs the event loop with the specified mode.
// See RunMode constants for available modes.
func LoopRun(loop *Loop, mode RunMode) error {
	var ret ffi.Arg
	ptr := unsafe.Pointer(loop)
	m := int32(mode)
	fnLoopRun.Call(&ret, &ptr, &m)
	if int32(ret) != 0 {
		return errors.New("xev_loop_run failed")
	}
	return nil
}

// LoopNow returns the loop's cached timestamp in milliseconds.
// This is a fast operation that doesn't make a system call.
// Call LoopUpdateNow to refresh the cached value.
func LoopNow(loop *Loop) int64 {
	var ret int64
	ptr := unsafe.Pointer(loop)
	fnLoopNow.Call(&ret, &ptr)
	return ret
}

// LoopUpdateNow refreshes the loop's cached timestamp.
// Call this if you need an accurate current time after doing non-I/O work.
func LoopUpdateNow(loop *Loop) {
	ptr := unsafe.Pointer(loop)
	fnLoopUpdateNow.Call(nil, &ptr)
}
