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

// TimerEvent is sent through the channel returned by RunChan.
type TimerEvent struct {
	Timer *Timer
	Err   error
}

// Timer provides high-level timer functionality.
// Create with NewTimer, schedule with RunFunc/RunWithHandler/RunChan.
type Timer struct {
	watcher    cxev.Watcher
	completion cxev.Completion
	handler    TimerHandler
	callbackID uintptr
	loop       *Loop
}

// NewTimer creates a new timer. Call Close when done.
func NewTimer() (*Timer, error) {
	t := &Timer{}
	if err := cxev.TimerInit(&t.watcher); err != nil {
		return nil, err
	}
	return t, nil
}

// Close releases timer resources and unregisters any callback.
func (t *Timer) Close() {
	if t.callbackID != 0 {
		cxev.UnregisterCallback(t.callbackID)
		t.callbackID = 0
	}
	cxev.TimerDeinit(&t.watcher)
}

// RunWithHandler schedules the timer with a handler interface.
// Use this when you need a stateful handler.
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
// This is the most common and convenient way to use timers.
//
// Return Stop to fire once, or Continue for a repeating timer.
func (t *Timer) RunFunc(loop *Loop, delay time.Duration, fn func(t *Timer, result error) Action) error {
	return t.RunWithHandler(loop, delay, TimerFunc(fn))
}

// RunChan schedules the timer and returns a channel that receives the event.
// The channel is closed after the timer fires once.
// Useful for select-based patterns or one-shot timers.
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
