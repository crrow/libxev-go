# libxev-go Wrapper Design

## Overview

Go wrapper for libxev, providing both low-level CGO bindings and idiomatic Go API.

## Scope (v1)

- Loop + Timer only (minimal set to validate architecture)
- Static linking with `libxev.a`

## Package Structure

```
libxev-go/
├── pkg/
│   ├── cxev/           # Low-level CGO bindings (public)
│   │   ├── cxev.go     # CGO directives + C types mapping
│   │   ├── loop.go     # xev_loop_* function bindings
│   │   ├── timer.go    # xev_timer_* function bindings
│   │   └── callback.go # Callback mechanism (export + userdata management)
│   │
│   └── xev/            # High-level Idiomatic Go API (public)
│       ├── loop.go     # Loop type wrapper
│       ├── timer.go    # Timer type wrapper
│       ├── handler.go  # Interface mode definitions
│       └── options.go  # Configuration options
│
├── deps/libxev/        # git submodule
└── example/            # Usage examples
```

## Low-level API: `pkg/cxev/`

### CGO Configuration

```go
/*
#cgo CFLAGS: -I${SRCDIR}/../../deps/libxev/include
#cgo LDFLAGS: -L${SRCDIR}/../../deps/libxev/zig-out/lib -lxev
#include <xev.h>
*/
import "C"
```

### Type Mappings

```go
type Loop C.xev_loop
type Completion C.xev_completion
type Watcher C.xev_watcher

type RunMode C.xev_run_mode_t
const (
    RunNoWait    RunMode = C.XEV_RUN_NO_WAIT
    RunOnce      RunMode = C.XEV_RUN_ONCE
    RunUntilDone RunMode = C.XEV_RUN_UNTIL_DONE
)

type CbAction C.xev_cb_action
const (
    Disarm CbAction = C.XEV_DISARM
    Rearm  CbAction = C.XEV_REARM
)
```

### Function Bindings

```go
// Loop
func LoopInit(loop *Loop) error
func LoopDeinit(loop *Loop)
func LoopRun(loop *Loop, mode RunMode) error
func LoopNow(loop *Loop) int64

// Timer
func TimerInit(watcher *Watcher) error
func TimerDeinit(watcher *Watcher)
func TimerRun(watcher *Watcher, loop *Loop, c *Completion, delayMs uint64, userdata unsafe.Pointer, cb TimerCallback)
```

### Callback Mechanism

```go
type TimerCallback func(loop *Loop, c *Completion, result int, userdata unsafe.Pointer) CbAction

var callbackRegistry sync.Map  // uintptr -> TimerCallback

//export goTimerCallback
func goTimerCallback(l *C.xev_loop, c *C.xev_completion, result C.int, userdata unsafe.Pointer) C.xev_cb_action {
    // Lookup and invoke Go callback from registry
}
```

## High-level API: `pkg/xev/`

### Loop

```go
type Loop struct {
    inner cxev.Loop
}

func NewLoop() (*Loop, error)
func (l *Loop) Close()
func (l *Loop) Run() error           // RunUntilDone
func (l *Loop) RunOnce() error       // RunOnce
func (l *Loop) Poll() error          // RunNoWait
func (l *Loop) Now() time.Duration
```

### Timer - Interface Mode

```go
type TimerHandler interface {
    OnTimer(t *Timer, result error) Action
}

type Action int
const (
    Stop     Action = iota  // Maps to Disarm
    Continue                // Maps to Rearm
)

type Timer struct { ... }

func NewTimer() (*Timer, error)
func (t *Timer) Close()
func (t *Timer) RunWithHandler(loop *Loop, delay time.Duration, handler TimerHandler) error
```

### Timer - Channel Mode

```go
type TimerEvent struct {
    Timer  *Timer
    Result error
}

func (t *Timer) RunChan(loop *Loop, delay time.Duration) (<-chan TimerEvent, error)
```

## Examples

### Channel Mode

```go
func main() {
    loop, _ := xev.NewLoop()
    defer loop.Close()

    timer, _ := xev.NewTimer()
    defer timer.Close()

    events, _ := timer.RunChan(loop, 5*time.Second)

    go func() {
        loop.Run()
    }()

    evt := <-events
    if evt.Result != nil {
        log.Fatal(evt.Result)
    }
    fmt.Println("Timer fired!")
}
```

### Interface Mode

```go
type myHandler struct{}

func (h *myHandler) OnTimer(t *xev.Timer, result error) xev.Action {
    fmt.Println("Timer fired!")
    return xev.Stop
}

func main() {
    loop, _ := xev.NewLoop()
    defer loop.Close()

    timer, _ := xev.NewTimer()
    defer timer.Close()

    timer.RunWithHandler(loop, 5*time.Second, &myHandler{})
    loop.Run()
}
```

### Raw Low-level API

```go
func main() {
    var loop cxev.Loop
    var watcher cxev.Watcher
    var completion cxev.Completion

    cxev.LoopInit(&loop)
    defer cxev.LoopDeinit(&loop)

    cxev.TimerInit(&watcher)
    defer cxev.TimerDeinit(&watcher)

    cxev.TimerRun(&watcher, &loop, &completion, 5000, nil, myCallback)
    cxev.LoopRun(&loop, cxev.RunUntilDone)
}
```

## Future Extensions

- Async watcher (`xev_async_*`)
- ThreadPool (`xev_threadpool_*`)
- Cross-platform build tags
- Dynamic linking option
