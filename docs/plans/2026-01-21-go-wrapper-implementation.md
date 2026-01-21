# libxev-go Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement Go bindings for libxev with low-level CGO layer (`pkg/cxev`) and high-level idiomatic API (`pkg/xev`).

**Architecture:** Two-layer design - `pkg/cxev` exposes raw C API with direct type mappings, `pkg/xev` wraps it with Go idioms (error handling, `time.Duration`, channel/interface patterns).

**Tech Stack:** Go 1.25+, CGO, libxev (static linking)

**License Header:** (for all .go files)
```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */
```

---

## Task 1: Low-level CGO Setup (`pkg/cxev/cxev.go`)

**Files:**
- Create: `pkg/cxev/cxev.go`

**Step 1: Create the CGO base file with directives and type definitions**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

/*
#cgo CFLAGS: -I${SRCDIR}/../../deps/libxev/include
#cgo LDFLAGS: -L${SRCDIR}/../../deps/libxev/zig-out/lib -lxev
#include <xev.h>
*/
import "C"

// Loop is the opaque event loop type.
type Loop C.xev_loop

// Completion represents a completion token for async operations.
type Completion C.xev_completion

// Watcher is a generic watcher type used by timers, async, etc.
type Watcher C.xev_watcher

// RunMode specifies how the event loop should run.
type RunMode C.xev_run_mode_t

const (
	RunNoWait    RunMode = C.XEV_RUN_NO_WAIT
	RunOnce      RunMode = C.XEV_RUN_ONCE
	RunUntilDone RunMode = C.XEV_RUN_UNTIL_DONE
)

// CbAction is the return value from callbacks indicating what to do next.
type CbAction C.xev_cb_action

const (
	Disarm CbAction = C.XEV_DISARM
	Rearm  CbAction = C.XEV_REARM
)

// CompletionState represents the state of a completion.
type CompletionState C.xev_completion_state_t

const (
	CompletionDead   CompletionState = C.XEV_COMPLETION_DEAD
	CompletionActive CompletionState = C.XEV_COMPLETION_ACTIVE
)
```

**Step 2: Verify it compiles**

Run: `go build ./pkg/cxev/`
Expected: No errors

**Step 3: Commit**

```bash
git add pkg/cxev/cxev.go
git commit -m "feat(cxev): add CGO setup and type definitions"
```

---

## Task 2: Loop Bindings (`pkg/cxev/loop.go`)

**Files:**
- Create: `pkg/cxev/loop.go`
- Test: `pkg/cxev/loop_test.go`

**Step 1: Write the failing test**

```go
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

	// RunNoWait should return immediately with no pending work
	if err := LoopRun(&loop, RunNoWait); err != nil {
		t.Fatalf("LoopRun failed: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/cxev/ -v -run TestLoop`
Expected: FAIL with "undefined: LoopInit"

**Step 3: Write the implementation**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

/*
#include <xev.h>
*/
import "C"
import "errors"

// LoopInit initializes an event loop.
func LoopInit(loop *Loop) error {
	ret := C.xev_loop_init((*C.xev_loop)(loop))
	if ret != 0 {
		return errors.New("xev_loop_init failed")
	}
	return nil
}

// LoopDeinit deinitializes an event loop.
func LoopDeinit(loop *Loop) {
	C.xev_loop_deinit((*C.xev_loop)(loop))
}

// LoopRun runs the event loop with the specified mode.
func LoopRun(loop *Loop, mode RunMode) error {
	ret := C.xev_loop_run((*C.xev_loop)(loop), C.xev_run_mode_t(mode))
	if ret != 0 {
		return errors.New("xev_loop_run failed")
	}
	return nil
}

// LoopNow returns the current cached time in milliseconds.
func LoopNow(loop *Loop) int64 {
	return int64(C.xev_loop_now((*C.xev_loop)(loop)))
}

// LoopUpdateNow updates the cached time.
func LoopUpdateNow(loop *Loop) {
	C.xev_loop_update_now((*C.xev_loop)(loop))
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/cxev/ -v -run TestLoop`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/cxev/loop.go pkg/cxev/loop_test.go
git commit -m "feat(cxev): add loop bindings"
```

---

## Task 3: Completion Bindings (`pkg/cxev/completion.go`)

**Files:**
- Create: `pkg/cxev/completion.go`
- Modify: `pkg/cxev/loop_test.go` (add completion test)

**Step 1: Write the failing test**

Add to `pkg/cxev/loop_test.go`:

```go
func TestCompletionZeroAndState(t *testing.T) {
	var c Completion
	CompletionZero(&c)

	state := CompletionState_(&c)
	if state != CompletionDead {
		t.Errorf("expected CompletionDead, got %v", state)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/cxev/ -v -run TestCompletion`
Expected: FAIL with "undefined: CompletionZero"

**Step 3: Write the implementation**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

/*
#include <xev.h>
*/
import "C"

// CompletionZero zeros a completion struct.
func CompletionZero(c *Completion) {
	C.xev_completion_zero((*C.xev_completion)(c))
}

// CompletionState_ returns the state of a completion.
// Named with underscore to avoid conflict with CompletionState type.
func CompletionState_(c *Completion) CompletionState {
	return CompletionState(C.xev_completion_state((*C.xev_completion)(c)))
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/cxev/ -v -run TestCompletion`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/cxev/completion.go pkg/cxev/loop_test.go
git commit -m "feat(cxev): add completion bindings"
```

---

## Task 4: Callback Registry (`pkg/cxev/callback.go`)

**Files:**
- Create: `pkg/cxev/callback.go`

**Step 1: Create callback infrastructure**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

/*
#include <xev.h>

// Forward declaration for Go callback
extern xev_cb_action goTimerCallback(xev_loop* l, xev_completion* c, int result, void* userdata);
*/
import "C"
import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// TimerCallback is the Go function signature for timer callbacks.
type TimerCallback func(loop *Loop, c *Completion, result int) CbAction

// callbackEntry stores a callback with its associated data.
type callbackEntry struct {
	timerCb TimerCallback
}

var (
	callbackMu       sync.RWMutex
	callbackRegistry = make(map[uintptr]*callbackEntry)
	callbackCounter  atomic.Uint64
)

// registerCallback registers a callback and returns a unique ID.
func registerCallback(entry *callbackEntry) uintptr {
	id := uintptr(callbackCounter.Add(1))
	callbackMu.Lock()
	callbackRegistry[id] = entry
	callbackMu.Unlock()
	return id
}

// unregisterCallback removes a callback from the registry.
func unregisterCallback(id uintptr) {
	callbackMu.Lock()
	delete(callbackRegistry, id)
	callbackMu.Unlock()
}

// getCallback retrieves a callback entry by ID.
func getCallback(id uintptr) *callbackEntry {
	callbackMu.RLock()
	entry := callbackRegistry[id]
	callbackMu.RUnlock()
	return entry
}

//export goTimerCallback
func goTimerCallback(l *C.xev_loop, c *C.xev_completion, result C.int, userdata unsafe.Pointer) C.xev_cb_action {
	id := uintptr(userdata)
	entry := getCallback(id)
	if entry == nil || entry.timerCb == nil {
		return C.xev_cb_action(Disarm)
	}

	action := entry.timerCb((*Loop)(l), (*Completion)(c), int(result))

	// If disarming, unregister the callback
	if action == Disarm {
		unregisterCallback(id)
	}

	return C.xev_cb_action(action)
}

// GetTimerCallbackPtr returns the C function pointer for timer callbacks.
func GetTimerCallbackPtr() C.xev_timer_cb {
	return C.xev_timer_cb(C.goTimerCallback)
}
```

**Step 2: Verify it compiles**

Run: `go build ./pkg/cxev/`
Expected: No errors

**Step 3: Commit**

```bash
git add pkg/cxev/callback.go
git commit -m "feat(cxev): add callback registry infrastructure"
```

---

## Task 5: Timer Bindings (`pkg/cxev/timer.go`)

**Files:**
- Create: `pkg/cxev/timer.go`
- Create: `pkg/cxev/timer_test.go`

**Step 1: Write the failing test**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

import (
	"sync/atomic"
	"testing"
)

func TestTimerInitDeinit(t *testing.T) {
	var w Watcher
	if err := TimerInit(&w); err != nil {
		t.Fatalf("TimerInit failed: %v", err)
	}
	TimerDeinit(&w)
}

func TestTimerRun(t *testing.T) {
	var loop Loop
	if err := LoopInit(&loop); err != nil {
		t.Fatalf("LoopInit failed: %v", err)
	}
	defer LoopDeinit(&loop)

	var w Watcher
	if err := TimerInit(&w); err != nil {
		t.Fatalf("TimerInit failed: %v", err)
	}
	defer TimerDeinit(&w)

	var c Completion
	var called atomic.Bool

	TimerRun(&w, &loop, &c, 10, func(loop *Loop, c *Completion, result int) CbAction {
		called.Store(true)
		return Disarm
	})

	if err := LoopRun(&loop, RunUntilDone); err != nil {
		t.Fatalf("LoopRun failed: %v", err)
	}

	if !called.Load() {
		t.Error("timer callback was not called")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/cxev/ -v -run TestTimer`
Expected: FAIL with "undefined: TimerInit"

**Step 3: Write the implementation**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

/*
#include <xev.h>

extern xev_cb_action goTimerCallback(xev_loop* l, xev_completion* c, int result, void* userdata);
*/
import "C"
import (
	"errors"
	"unsafe"
)

// TimerInit initializes a timer watcher.
func TimerInit(w *Watcher) error {
	ret := C.xev_timer_init((*C.xev_watcher)(w))
	if ret != 0 {
		return errors.New("xev_timer_init failed")
	}
	return nil
}

// TimerDeinit deinitializes a timer watcher.
func TimerDeinit(w *Watcher) {
	C.xev_timer_deinit((*C.xev_watcher)(w))
}

// TimerRun starts a timer that fires after delayMs milliseconds.
func TimerRun(w *Watcher, loop *Loop, c *Completion, delayMs uint64, cb TimerCallback) {
	entry := &callbackEntry{timerCb: cb}
	id := registerCallback(entry)

	C.xev_timer_run(
		(*C.xev_watcher)(w),
		(*C.xev_loop)(loop),
		(*C.xev_completion)(c),
		C.uint64_t(delayMs),
		unsafe.Pointer(id),
		C.xev_timer_cb(C.goTimerCallback),
	)
}

// TimerCancel cancels a pending timer.
func TimerCancel(w *Watcher, loop *Loop, c *Completion, cCancel *Completion, cb TimerCallback) {
	entry := &callbackEntry{timerCb: cb}
	id := registerCallback(entry)

	C.xev_timer_cancel(
		(*C.xev_watcher)(w),
		(*C.xev_loop)(loop),
		(*C.xev_completion)(c),
		(*C.xev_completion)(cCancel),
		unsafe.Pointer(id),
		C.xev_timer_cb(C.goTimerCallback),
	)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/cxev/ -v -run TestTimer`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/cxev/timer.go pkg/cxev/timer_test.go
git commit -m "feat(cxev): add timer bindings"
```

---

## Task 6: High-level Loop (`pkg/xev/loop.go`)

**Files:**
- Create: `pkg/xev/loop.go`
- Create: `pkg/xev/loop_test.go`

**Step 1: Write the failing test**

```go
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

func TestNewLoop(t *testing.T) {
	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()
}

func TestLoopNow(t *testing.T) {
	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	now := loop.Now()
	if now < 0 {
		t.Errorf("Now returned negative duration: %v", now)
	}
}

func TestLoopPoll(t *testing.T) {
	loop, err := NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	// Poll should return immediately with no pending work
	start := time.Now()
	if err := loop.Poll(); err != nil {
		t.Fatalf("Poll failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("Poll took too long: %v", elapsed)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/xev/ -v -run TestLoop -run TestNew`
Expected: FAIL with "undefined: NewLoop"

**Step 3: Write the implementation**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"time"

	"github.com/crrow/libxev-go/pkg/cxev"
)

// Loop is a high-level wrapper around the xev event loop.
type Loop struct {
	inner cxev.Loop
}

// NewLoop creates and initializes a new event loop.
func NewLoop() (*Loop, error) {
	l := &Loop{}
	if err := cxev.LoopInit(&l.inner); err != nil {
		return nil, err
	}
	return l, nil
}

// Close deinitializes the event loop.
func (l *Loop) Close() {
	cxev.LoopDeinit(&l.inner)
}

// Run runs the event loop until all work is complete.
func (l *Loop) Run() error {
	return cxev.LoopRun(&l.inner, cxev.RunUntilDone)
}

// RunOnce runs a single iteration of the event loop.
func (l *Loop) RunOnce() error {
	return cxev.LoopRun(&l.inner, cxev.RunOnce)
}

// Poll checks for ready events without blocking.
func (l *Loop) Poll() error {
	return cxev.LoopRun(&l.inner, cxev.RunNoWait)
}

// Now returns the cached time of the event loop.
func (l *Loop) Now() time.Duration {
	ms := cxev.LoopNow(&l.inner)
	return time.Duration(ms) * time.Millisecond
}

// UpdateNow updates the cached time.
func (l *Loop) UpdateNow() {
	cxev.LoopUpdateNow(&l.inner)
}

// Inner returns the underlying cxev.Loop for advanced usage.
func (l *Loop) Inner() *cxev.Loop {
	return &l.inner
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/xev/ -v -run TestLoop -run TestNew`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/xev/loop.go pkg/xev/loop_test.go
git commit -m "feat(xev): add high-level Loop wrapper"
```

---

## Task 7: High-level Timer with Handler Interface (`pkg/xev/timer.go`)

**Files:**
- Create: `pkg/xev/handler.go`
- Create: `pkg/xev/timer.go`
- Create: `pkg/xev/timer_test.go`

**Step 1: Create handler interface**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

// Action indicates what to do after a callback.
type Action int

const (
	// Stop disarms the watcher (no more callbacks).
	Stop Action = iota
	// Continue rearms the watcher for more callbacks.
	Continue
)

// TimerHandler is the interface for handling timer events.
type TimerHandler interface {
	OnTimer(t *Timer, result error) Action
}

// TimerFunc is a function adapter for TimerHandler.
type TimerFunc func(t *Timer, result error) Action

// OnTimer implements TimerHandler.
func (f TimerFunc) OnTimer(t *Timer, result error) Action {
	return f(t, result)
}
```

**Step 2: Write the failing test**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"sync/atomic"
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

	var called atomic.Bool
	handler := TimerFunc(func(t *Timer, result error) Action {
		called.Store(true)
		return Stop
	})

	if err := timer.RunWithHandler(loop, 10*time.Millisecond, handler); err != nil {
		t.Fatalf("RunWithHandler failed: %v", err)
	}

	if err := loop.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !called.Load() {
		t.Error("handler was not called")
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

	var count atomic.Int32
	if err := timer.RunFunc(loop, 5*time.Millisecond, func(t *Timer, result error) Action {
		if count.Add(1) >= 3 {
			return Stop
		}
		return Continue
	}); err != nil {
		t.Fatalf("RunFunc failed: %v", err)
	}

	if err := loop.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if c := count.Load(); c != 3 {
		t.Errorf("expected 3 calls, got %d", c)
	}
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./pkg/xev/ -v -run TestTimer`
Expected: FAIL with "undefined: NewTimer"

**Step 4: Write the implementation**

```go
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

// Timer is a high-level wrapper around the xev timer.
type Timer struct {
	watcher    cxev.Watcher
	completion cxev.Completion
	handler    TimerHandler
	delayMs    uint64
}

// NewTimer creates a new timer.
func NewTimer() (*Timer, error) {
	t := &Timer{}
	if err := cxev.TimerInit(&t.watcher); err != nil {
		return nil, err
	}
	return t, nil
}

// Close deinitializes the timer.
func (t *Timer) Close() {
	cxev.TimerDeinit(&t.watcher)
}

// RunWithHandler starts the timer with a handler interface.
func (t *Timer) RunWithHandler(loop *Loop, delay time.Duration, handler TimerHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}
	t.handler = handler
	t.delayMs = uint64(delay.Milliseconds())

	cxev.TimerRun(&t.watcher, &loop.inner, &t.completion, t.delayMs, t.callback)
	return nil
}

// RunFunc starts the timer with a callback function.
func (t *Timer) RunFunc(loop *Loop, delay time.Duration, fn func(t *Timer, result error) Action) error {
	return t.RunWithHandler(loop, delay, TimerFunc(fn))
}

// callback is the internal callback that bridges to the handler.
func (t *Timer) callback(loop *cxev.Loop, c *cxev.Completion, result int) cxev.CbAction {
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
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/xev/ -v -run TestTimer`
Expected: PASS

**Step 6: Commit**

```bash
git add pkg/xev/handler.go pkg/xev/timer.go pkg/xev/timer_test.go
git commit -m "feat(xev): add Timer with handler interface"
```

---

## Task 8: Timer Channel Mode (`pkg/xev/timer.go`)

**Files:**
- Modify: `pkg/xev/timer.go`
- Modify: `pkg/xev/timer_test.go`

**Step 1: Write the failing test**

Add to `pkg/xev/timer_test.go`:

```go
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

	events, err := timer.RunChan(loop, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("RunChan failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		loop.Run()
		close(done)
	}()

	select {
	case evt := <-events:
		if evt.Err != nil {
			t.Errorf("unexpected error: %v", evt.Err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for timer event")
	}

	<-done
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/xev/ -v -run TestTimerChannel`
Expected: FAIL with "timer.RunChan undefined"

**Step 3: Add channel mode to timer.go**

Add to `pkg/xev/timer.go`:

```go
// TimerEvent is sent on the channel when a timer fires.
type TimerEvent struct {
	Timer *Timer
	Err   error
}

// RunChan starts the timer and returns a channel that receives events.
// The timer fires once and then stops (Disarm behavior).
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/xev/ -v -run TestTimerChannel`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/xev/timer.go pkg/xev/timer_test.go
git commit -m "feat(xev): add Timer channel mode"
```

---

## Task 9: Example - Timer with Handler (`example/timer_handler/`)

**Files:**
- Create: `example/timer_handler/main.go`

**Step 1: Create the example**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/crrow/libxev-go/pkg/xev"
)

type myHandler struct {
	count int
}

func (h *myHandler) OnTimer(t *xev.Timer, result error) xev.Action {
	if result != nil {
		log.Printf("Timer error: %v", result)
		return xev.Stop
	}

	h.count++
	fmt.Printf("Timer fired! Count: %d\n", h.count)

	if h.count >= 3 {
		fmt.Println("Stopping after 3 fires")
		return xev.Stop
	}
	return xev.Continue
}

func main() {
	loop, err := xev.NewLoop()
	if err != nil {
		log.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	timer, err := xev.NewTimer()
	if err != nil {
		log.Fatalf("NewTimer failed: %v", err)
	}
	defer timer.Close()

	if err := timer.RunWithHandler(loop, 500*time.Millisecond, &myHandler{}); err != nil {
		log.Fatalf("RunWithHandler failed: %v", err)
	}

	fmt.Println("Starting event loop...")
	if err := loop.Run(); err != nil {
		log.Fatalf("Run failed: %v", err)
	}
	fmt.Println("Event loop finished")
}
```

**Step 2: Verify it compiles and runs**

Run: `go run ./example/timer_handler/`
Expected: Prints "Timer fired!" 3 times then exits

**Step 3: Commit**

```bash
git add example/timer_handler/main.go
git commit -m "example: add timer handler example"
```

---

## Task 10: Example - Timer with Channel (`example/timer_chan/`)

**Files:**
- Create: `example/timer_chan/main.go`

**Step 1: Create the example**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/crrow/libxev-go/pkg/xev"
)

func main() {
	loop, err := xev.NewLoop()
	if err != nil {
		log.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	timer, err := xev.NewTimer()
	if err != nil {
		log.Fatalf("NewTimer failed: %v", err)
	}
	defer timer.Close()

	events, err := timer.RunChan(loop, 1*time.Second)
	if err != nil {
		log.Fatalf("RunChan failed: %v", err)
	}

	// Run loop in background
	done := make(chan struct{})
	go func() {
		if err := loop.Run(); err != nil {
			log.Printf("Run error: %v", err)
		}
		close(done)
	}()

	fmt.Println("Waiting for timer (1 second)...")

	select {
	case evt := <-events:
		if evt.Err != nil {
			log.Fatalf("Timer error: %v", evt.Err)
		}
		fmt.Println("Timer fired!")
	case <-time.After(5 * time.Second):
		log.Fatal("Timeout waiting for timer")
	}

	<-done
	fmt.Println("Done")
}
```

**Step 2: Verify it compiles and runs**

Run: `go run ./example/timer_chan/`
Expected: Waits 1 second, prints "Timer fired!", then exits

**Step 3: Commit**

```bash
git add example/timer_chan/main.go
git commit -m "example: add timer channel example"
```

---

## Task 11: Example - Raw Low-level API (`example/timer_raw/`)

**Files:**
- Create: `example/timer_raw/main.go`

**Step 1: Create the example**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"fmt"
	"log"

	"github.com/crrow/libxev-go/pkg/cxev"
)

func main() {
	var loop cxev.Loop
	if err := cxev.LoopInit(&loop); err != nil {
		log.Fatalf("LoopInit failed: %v", err)
	}
	defer cxev.LoopDeinit(&loop)

	var watcher cxev.Watcher
	if err := cxev.TimerInit(&watcher); err != nil {
		log.Fatalf("TimerInit failed: %v", err)
	}
	defer cxev.TimerDeinit(&watcher)

	var completion cxev.Completion

	fmt.Println("Starting 1 second timer (raw API)...")

	cxev.TimerRun(&watcher, &loop, &completion, 1000, func(l *cxev.Loop, c *cxev.Completion, result int) cxev.CbAction {
		fmt.Println("Timer fired! (raw callback)")
		return cxev.Disarm
	})

	if err := cxev.LoopRun(&loop, cxev.RunUntilDone); err != nil {
		log.Fatalf("LoopRun failed: %v", err)
	}

	fmt.Println("Done")
}
```

**Step 2: Verify it compiles and runs**

Run: `go run ./example/timer_raw/`
Expected: Waits 1 second, prints "Timer fired!", then exits

**Step 3: Commit**

```bash
git add example/timer_raw/main.go
git commit -m "example: add raw timer example"
```

---

## Task 12: Final Integration Test

**Files:**
- Create: `pkg/xev/integration_test.go`

**Step 1: Write integration test**

```go
/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/crrow/libxev-go/pkg/xev"
)

func TestIntegration_MultipleTimers(t *testing.T) {
	loop, err := xev.NewLoop()
	if err != nil {
		t.Fatalf("NewLoop failed: %v", err)
	}
	defer loop.Close()

	var count atomic.Int32

	for i := 0; i < 3; i++ {
		timer, err := xev.NewTimer()
		if err != nil {
			t.Fatalf("NewTimer failed: %v", err)
		}
		defer timer.Close()

		delay := time.Duration((i+1)*10) * time.Millisecond
		if err := timer.RunFunc(loop, delay, func(t *xev.Timer, result error) xev.Action {
			count.Add(1)
			return xev.Stop
		}); err != nil {
			t.Fatalf("RunFunc failed: %v", err)
		}
	}

	if err := loop.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if c := count.Load(); c != 3 {
		t.Errorf("expected 3 timer fires, got %d", c)
	}
}
```

**Step 2: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass

**Step 3: Commit**

```bash
git add pkg/xev/integration_test.go
git commit -m "test: add integration test for multiple timers"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | CGO setup | `pkg/cxev/cxev.go` |
| 2 | Loop bindings | `pkg/cxev/loop.go`, test |
| 3 | Completion bindings | `pkg/cxev/completion.go` |
| 4 | Callback registry | `pkg/cxev/callback.go` |
| 5 | Timer bindings | `pkg/cxev/timer.go`, test |
| 6 | High-level Loop | `pkg/xev/loop.go`, test |
| 7 | Timer handler mode | `pkg/xev/handler.go`, `pkg/xev/timer.go`, test |
| 8 | Timer channel mode | modify `pkg/xev/timer.go` |
| 9 | Handler example | `example/timer_handler/` |
| 10 | Channel example | `example/timer_chan/` |
| 11 | Raw example | `example/timer_raw/` |
| 12 | Integration test | `pkg/xev/integration_test.go` |
