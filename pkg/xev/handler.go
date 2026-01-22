/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

// Action controls the behavior of a watcher after its callback returns.
//
// Every event callback must return an Action to indicate whether the watcher
// should continue firing or be stopped. This provides explicit control over
// one-shot vs. repeating operations.
type Action int

const (
	// Stop disarms the watcher after the callback returns.
	// The watcher will not fire again until explicitly re-armed.
	// Use this for one-shot operations or when you want to stop a repeating timer.
	Stop Action = iota

	// Continue keeps the watcher active after the callback returns.
	// For timers, this causes the timer to reset and fire again after the same interval.
	// For I/O operations, this re-arms the read/write for the next event.
	Continue
)

// TimerHandler is the interface for handling timer events.
//
// Implement this interface when you need stateful timer handling, such as
// tracking how many times a timer has fired or implementing exponential backoff.
//
// For simple use cases, [TimerFunc] provides a more convenient functional approach.
//
// Example implementation:
//
//	type CountingTimer struct {
//	    count int
//	    max   int
//	}
//
//	func (c *CountingTimer) OnTimer(t *xev.Timer, err error) xev.Action {
//	    c.count++
//	    if c.count >= c.max {
//	        return xev.Stop
//	    }
//	    return xev.Continue
//	}
type TimerHandler interface {
	// OnTimer is called when the timer fires.
	// Return [Stop] to prevent further firings, or [Continue] for repeating timers.
	OnTimer(t *Timer, result error) Action
}

// TimerFunc is a function adapter for [TimerHandler].
//
// This allows ordinary functions to be used as timer handlers, which is the
// most common pattern for simple timer callbacks.
//
// Example:
//
//	timer.RunFunc(loop, time.Second, func(t *xev.Timer, err error) xev.Action {
//	    fmt.Println("Timer fired!")
//	    return xev.Stop // One-shot timer
//	})
type TimerFunc func(t *Timer, result error) Action

// OnTimer implements [TimerHandler].
func (f TimerFunc) OnTimer(t *Timer, result error) Action {
	return f(t, result)
}
