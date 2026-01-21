/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"testing"
	"time"
)

func TestTimerWithHandler(t *testing.T) {
	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	timer, err := NewTimer()
	if err != nil {
		t.Fatalf("NewTimer failed: %v", err)
	}
	defer timer.Close()

	fired := false
	handler := TimerFunc(func(timer *Timer, result error) Action {
		fired = true
		return Stop
	})

	if err := timer.RunWithHandler(loop, 10*time.Millisecond, handler); err != nil {
		t.Fatalf("RunWithHandler failed: %v", err)
	}

	if err := loop.Run(); err != nil {
		t.Fatalf("Loop.Run failed: %v", err)
	}

	if !fired {
		t.Error("timer was not fired")
	}
}

func TestTimerWithFunc(t *testing.T) {
	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	timer, err := NewTimer()
	if err != nil {
		t.Fatalf("NewTimer failed: %v", err)
	}
	defer timer.Close()

	fired := false
	err = timer.RunFunc(loop, 10*time.Millisecond, func(t *Timer, result error) Action {
		fired = true
		return Stop
	})
	if err != nil {
		t.Fatalf("RunFunc failed: %v", err)
	}

	if err := loop.Run(); err != nil {
		t.Fatalf("Loop.Run failed: %v", err)
	}

	if !fired {
		t.Error("timer was not fired")
	}
}

func TestTimerChannel(t *testing.T) {
	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	timer, err := NewTimer()
	if err != nil {
		t.Fatalf("NewTimer failed: %v", err)
	}
	defer timer.Close()

	ch, err := timer.RunChan(loop, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("RunChan failed: %v", err)
	}

	go func() {
		loop.Run()
	}()

	evt := <-ch
	if evt.Err != nil {
		t.Errorf("timer event has error: %v", evt.Err)
	}
	if evt.Timer != timer {
		t.Error("timer event has wrong timer")
	}
}
