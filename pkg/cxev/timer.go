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

// FFI function descriptors for timer operations.
var (
	fnTimerInit   ffi.Fun
	fnTimerDeinit ffi.Fun
	fnTimerRun    ffi.Fun
	fnTimerReset  ffi.Fun
	fnTimerCancel ffi.Fun
)

func registerTimerFunctions() error {
	var err error

	// int xev_timer_init(xev_timer* timer)
	fnTimerInit, err = lib.Prep("xev_timer_init", &ffi.TypeSint32, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_timer_deinit(xev_timer* timer)
	fnTimerDeinit, err = lib.Prep("xev_timer_deinit", &ffi.TypeVoid, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_timer_run(xev_timer*, xev_loop*, xev_completion*, uint64_t next_ms, void* userdata, callback_fn)
	fnTimerRun, err = lib.Prep("xev_timer_run",
		&ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer,
		&ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_timer_reset(xev_timer*, xev_loop*, xev_completion*, xev_completion* cancel, uint64_t, void*, callback_fn)
	fnTimerReset, err = lib.Prep("xev_timer_reset",
		&ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer,
		&ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_timer_cancel(xev_timer*, xev_loop*, xev_completion*, xev_completion* cancel, void*, callback_fn)
	fnTimerCancel, err = lib.Prep("xev_timer_cancel",
		&ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer,
		&ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	return nil
}

// TimerInit initializes a timer watcher.
func TimerInit(w *Watcher) error {
	if loadErr != nil {
		return loadErr
	}
	var ret ffi.Arg
	ptr := unsafe.Pointer(w)
	fnTimerInit.Call(&ret, &ptr)
	if int32(ret) != 0 {
		return errors.New("xev_timer_init failed")
	}
	return nil
}

// TimerDeinit releases resources for a timer watcher.
func TimerDeinit(w *Watcher) {
	ptr := unsafe.Pointer(w)
	fnTimerDeinit.Call(nil, &ptr)
}

// TimerRun schedules a timer to fire after delayMs milliseconds.
//
// Parameters:
//   - w: Timer watcher (must be initialized)
//   - loop: Event loop to register with
//   - c: Completion to track this operation
//   - delayMs: Milliseconds until the timer fires
//   - userdata: Opaque value passed to callback (typically a callback registry ID)
//   - cb: C function pointer to invoke when timer fires
//
// The callback signature expected by libxev is:
//
//	int32_t callback(xev_loop*, xev_completion*, int32_t result, void* userdata)
//
// Returns CbAction (Disarm=0 to stop, Rearm=1 to repeat).
func TimerRun(w *Watcher, loop *Loop, c *Completion, delayMs uint64, userdata, cb uintptr) {
	wPtr := unsafe.Pointer(w)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	fnTimerRun.Call(nil, &wPtr, &loopPtr, &cPtr, &delayMs, &userdata, &cb)
}

// TimerReset modifies a running timer's delay.
// If the timer has already fired, this re-arms it.
// The cCancel completion is used internally by libxev for the cancel operation.
func TimerReset(w *Watcher, loop *Loop, c, cCancel *Completion, delayMs uint64, userdata, cb uintptr) {
	wPtr := unsafe.Pointer(w)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	cCancelPtr := unsafe.Pointer(cCancel)
	fnTimerReset.Call(nil, &wPtr, &loopPtr, &cPtr, &cCancelPtr, &delayMs, &userdata, &cb)
}

// TimerCancel cancels a pending timer.
// The callback will be invoked with a cancellation result.
func TimerCancel(w *Watcher, loop *Loop, c, cCancel *Completion, userdata, cb uintptr) {
	wPtr := unsafe.Pointer(w)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	cCancelPtr := unsafe.Pointer(cCancel)
	fnTimerCancel.Call(nil, &wPtr, &loopPtr, &cPtr, &cCancelPtr, &userdata, &cb)
}
