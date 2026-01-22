/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"errors"
	"time"

	"github.com/crrow/libxev-go/pkg/cxev"
)

// TimerEvent is the event delivered through the channel returned by [Timer.RunChan].
//
// This struct bundles the timer reference with any error that occurred,
// allowing channel-based patterns to handle both success and failure cases.
type TimerEvent struct {
	// Timer is the timer that fired.
	Timer *Timer
	// Err is non-nil if an error occurred (e.g., loop shutdown).
	Err error
}

// Timer provides high-level timer functionality for scheduling delayed or
// periodic callbacks.
//
// Timers are one-shot by default. To create a repeating timer, return [Continue]
// from the callback instead of [Stop].
//
// # Creating and Using Timers
//
// Create a timer with [NewTimer], schedule it with one of the Run methods,
// and clean up with [Timer.Close]:
//
//	timer, err := xev.NewTimer()
//	if err != nil {
//	    return err
//	}
//	defer timer.Close()
//
//	// One-shot timer
//	timer.RunFunc(loop, 100*time.Millisecond, func(t *xev.Timer, err error) xev.Action {
//	    fmt.Println("Timer fired!")
//	    return xev.Stop
//	})
//
//	// Repeating timer
//	timer.RunFunc(loop, time.Second, func(t *xev.Timer, err error) xev.Action {
//	    fmt.Println("Tick!")
//	    return xev.Continue
//	})
//
// # Scheduling Methods
//
// Three methods are available for scheduling timers, each suited to different patterns:
//
//   - [Timer.RunFunc]: Callback function (most common)
//   - [Timer.RunWithHandler]: Interface for stateful handlers
//   - [Timer.RunChan]: Channel for select-based patterns
//
// # Thread Safety
//
// Timer operations are not thread-safe. All operations on a Timer must be
// performed from the same goroutine that runs the [Loop].
type Timer struct {
	watcher    cxev.Watcher
	completion cxev.Completion
	handler    TimerHandler
	callbackID uintptr
	loop       *Loop
}

// NewTimer creates a new timer.
//
// The timer is not scheduled until one of the Run methods is called.
// Call [Timer.Close] when the timer is no longer needed to release resources.
//
// Returns an error if the timer cannot be initialized.
func NewTimer() (*Timer, error) {
	t := &Timer{}
	if err := cxev.TimerInit(&t.watcher); err != nil {
		return nil, err
	}
	return t, nil
}

// Close releases all resources associated with the timer.
//
// This must be called when the timer is no longer needed, even if it has
// already fired. Close unregisters any pending callbacks and releases
// the underlying watcher resources.
//
// It is safe to call Close on a timer that has already fired or was never
// scheduled.
func (t *Timer) Close() {
	if t.callbackID != 0 {
		cxev.UnregisterCallback(t.callbackID)
		t.callbackID = 0
	}
	cxev.TimerDeinit(&t.watcher)
}

// RunWithHandler schedules the timer with a [TimerHandler] interface.
//
// Use this method when you need a stateful handler that implements [TimerHandler].
// For simple callbacks, [Timer.RunFunc] is more convenient.
//
// The handler's OnTimer method will be called when the timer fires. Return [Stop]
// from OnTimer for a one-shot timer, or [Continue] for repeating timers.
//
// Returns an error if handler is nil.
func (t *Timer) RunWithHandler(loop *Loop, delay time.Duration, handler TimerHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}
	t.handler = handler
	t.loop = loop

	t.callbackID = cxev.TimerRunWithCallback(&t.watcher, &loop.inner, &t.completion, uint64(delay.Milliseconds()), t.callback)
	return nil
}

// RunFunc schedules the timer with a callback function.
//
// This is the most common and convenient way to use timers. The callback is
// invoked when the timer fires, receiving the timer instance and any error.
//
// Return [Stop] from the callback for a one-shot timer, or [Continue] to
// repeat with the same delay.
//
// Example:
//
//	// One-shot timer that fires after 100ms
//	timer.RunFunc(loop, 100*time.Millisecond, func(t *xev.Timer, err error) xev.Action {
//	    if err != nil {
//	        log.Printf("Timer error: %v", err)
//	        return xev.Stop
//	    }
//	    fmt.Println("Timer fired!")
//	    return xev.Stop
//	})
//
//	// Repeating timer that fires every second
//	timer.RunFunc(loop, time.Second, func(t *xev.Timer, err error) xev.Action {
//	    fmt.Println("Tick!")
//	    return xev.Continue
//	})
func (t *Timer) RunFunc(loop *Loop, delay time.Duration, fn func(t *Timer, result error) Action) error {
	return t.RunWithHandler(loop, delay, TimerFunc(fn))
}

// RunChan schedules the timer and returns a channel that receives the event.
//
// This method is useful for select-based patterns or integrating timers with
// other channel operations. The channel receives exactly one event when the
// timer fires, then is closed.
//
// Note: RunChan always creates a one-shot timer. For repeating timers, use
// [Timer.RunFunc] or [Timer.RunWithHandler] instead.
//
// Example:
//
//	ch, err := timer.RunChan(loop, time.Second)
//	if err != nil {
//	    return err
//	}
//
//	select {
//	case event := <-ch:
//	    if event.Err != nil {
//	        log.Printf("Timer error: %v", event.Err)
//	    } else {
//	        fmt.Println("Timer fired!")
//	    }
//	case <-ctx.Done():
//	    // Timeout or cancellation
//	}
func (t *Timer) RunChan(loop *Loop, delay time.Duration) (<-chan TimerEvent, error) {
	ch := make(chan TimerEvent, 1)

	handler := TimerFunc(func(timer *Timer, result error) Action {
		ch <- TimerEvent{Timer: timer, Err: result}
		close(ch)
		return Stop
	})

	if err := t.RunWithHandler(loop, delay, handler); err != nil {
		close(ch)
		return nil, err
	}

	return ch, nil
}

func (t *Timer) callback(loop *cxev.Loop, c *cxev.Completion, result int32, userdata uintptr) cxev.CbAction {
	var err error
	if result != 0 {
		err = errors.New("timer error")
	}

	action := t.handler.OnTimer(t, err)

	if action == Continue {
		return cxev.Rearm
	}
	return cxev.Disarm
}
