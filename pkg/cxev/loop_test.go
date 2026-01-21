/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

import "testing"

func TestLoopInitDeinit(t *testing.T) {
	var loop Loop
	if err := LoopInit(&loop); err != nil {
		t.Fatalf("LoopInit failed: %v", err)
	}
	LoopDeinit(&loop)
}

func TestLoopNow(t *testing.T) {
	var loop Loop
	if err := LoopInit(&loop); err != nil {
		t.Fatalf("LoopInit failed: %v", err)
	}
	defer LoopDeinit(&loop)

	now := LoopNow(&loop)
	if now < 0 {
		t.Errorf("LoopNow returned negative value: %d", now)
	}
}

func TestLoopRunNoWait(t *testing.T) {
	var loop Loop
	if err := LoopInit(&loop); err != nil {
		t.Fatalf("LoopInit failed: %v", err)
	}
	defer LoopDeinit(&loop)

	if err := LoopRun(&loop, RunNoWait); err != nil {
		t.Fatalf("LoopRun failed: %v", err)
	}
}

func TestTimerCallback(t *testing.T) {
	var loop Loop
	var watcher Watcher
	var completion Completion

	if err := LoopInit(&loop); err != nil {
		t.Fatalf("LoopInit failed: %v", err)
	}
	defer LoopDeinit(&loop)

	if err := TimerInit(&watcher); err != nil {
		t.Fatalf("TimerInit failed: %v", err)
	}
	defer TimerDeinit(&watcher)

	fired := false
	id := TimerRunWithCallback(&watcher, &loop, &completion, 10, func(l *Loop, c *Completion, result int32, userdata uintptr) CbAction {
		fired = true
		return Disarm
	})
	defer UnregisterCallback(id)

	if err := LoopRun(&loop, RunUntilDone); err != nil {
		t.Fatalf("LoopRun failed: %v", err)
	}

	if !fired {
		t.Error("timer callback was not fired")
	}
}
