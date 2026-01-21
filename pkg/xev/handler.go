/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

// Action controls what happens after a callback returns.
type Action int

const (
	// Stop disarms the watcher - it will not fire again.
	Stop Action = iota
	// Continue keeps the watcher active (e.g., for repeating timers).
	Continue
)

// TimerHandler is the interface for handling timer events.
// Implement this interface for complex timer logic with state.
type TimerHandler interface {
	OnTimer(t *Timer, result error) Action
}

// TimerFunc is an adapter that allows ordinary functions to be used as TimerHandler.
// This is the most common way to handle timer events.
type TimerFunc func(t *Timer, result error) Action

// OnTimer implements TimerHandler.
func (f TimerFunc) OnTimer(t *Timer, result error) Action {
	return f(t, result)
}
