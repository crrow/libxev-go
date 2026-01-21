# libxev-go Wrapper Design

## Overview

Go wrapper for libxev using **purego** (no CGO), providing both low-level bindings and idiomatic Go API.

## Why purego?

- **No CGO**: Faster builds, simpler cross-compilation, no C toolchain required
- **Pure Go**: Single binary distribution (with embedded or bundled dylib)
- **Callback support**: purego's `NewCallback` handles C→Go callbacks

## Scope (v1)

- Loop + Timer only (minimal set to validate architecture)
- Dynamic linking with `libxev.dylib` / `libxev.so`

## Package Structure

```
libxev-go/
├── pkg/
│   ├── cxev/           # Low-level purego bindings (public)
│   │   ├── cxev.go     # Library loading + type definitions
│   │   ├── loop.go     # xev_loop_* function bindings
│   │   ├── timer.go    # xev_timer_* function bindings
│   │   └── callback.go # Callback registry + purego.NewCallback
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

### Library Loading

```go
package cxev

import (
    "sync"
    "unsafe"
    "github.com/ebitengine/purego"
)

var (
    libxev  uintptr
    loadErr error
    once    sync.Once
)

func init() {
    once.Do(func() {
        // Platform-specific library name
        libxev, loadErr = purego.Dlopen(libPath(), purego.RTLD_NOW|purego.RTLD_GLOBAL)
        if loadErr != nil {
            return
        }
        registerFunctions()
    })
}

func libPath() string {
    // darwin: libxev.dylib, linux: libxev.so
    // Can be overridden via LIBXEV_PATH env var
}
```

### Type Mappings

```go
// Opaque types - sized byte arrays matching C struct sizes
const (
    SizeofLoop       = 512
    SizeofCompletion = 320
    SizeofWatcher    = 256
)

type Loop [SizeofLoop]byte
type Completion [SizeofCompletion]byte
type Watcher [SizeofWatcher]byte

type RunMode int32
const (
    RunNoWait    RunMode = 0
    RunOnce      RunMode = 1
    RunUntilDone RunMode = 2
)

type CbAction int32
const (
    Disarm CbAction = 0
    Rearm  CbAction = 1
)
```

### Function Bindings

```go
var (
    // Loop functions
    xev_loop_init   func(loop unsafe.Pointer) int32
    xev_loop_deinit func(loop unsafe.Pointer)
    xev_loop_run    func(loop unsafe.Pointer, mode int32) int32
    xev_loop_now    func(loop unsafe.Pointer) int64

    // Timer functions  
    xev_timer_init   func(w unsafe.Pointer) int32
    xev_timer_deinit func(w unsafe.Pointer)
    xev_timer_run    func(w, loop, c unsafe.Pointer, nextMs uint64, userdata, cb uintptr)
)

func registerFunctions() {
    purego.RegisterLibFunc(&xev_loop_init, libxev, "xev_loop_init")
    purego.RegisterLibFunc(&xev_loop_deinit, libxev, "xev_loop_deinit")
    purego.RegisterLibFunc(&xev_loop_run, libxev, "xev_loop_run")
    purego.RegisterLibFunc(&xev_loop_now, libxev, "xev_loop_now")
    
    purego.RegisterLibFunc(&xev_timer_init, libxev, "xev_timer_init")
    purego.RegisterLibFunc(&xev_timer_deinit, libxev, "xev_timer_deinit")
    purego.RegisterLibFunc(&xev_timer_run, libxev, "xev_timer_run")
}

// Public wrappers
func LoopInit(loop *Loop) error {
    if ret := xev_loop_init(unsafe.Pointer(loop)); ret != 0 {
        return fmt.Errorf("xev_loop_init failed: %d", ret)
    }
    return nil
}

func LoopDeinit(loop *Loop)              { xev_loop_deinit(unsafe.Pointer(loop)) }
func LoopRun(loop *Loop, mode RunMode) error {
    if ret := xev_loop_run(unsafe.Pointer(loop), int32(mode)); ret != 0 {
        return fmt.Errorf("xev_loop_run failed: %d", ret)
    }
    return nil
}
func LoopNow(loop *Loop) int64           { return xev_loop_now(unsafe.Pointer(loop)) }

func TimerInit(w *Watcher) error {
    if ret := xev_timer_init(unsafe.Pointer(w)); ret != 0 {
        return fmt.Errorf("xev_timer_init failed: %d", ret)
    }
    return nil
}
func TimerDeinit(w *Watcher)             { xev_timer_deinit(unsafe.Pointer(w)) }
```

### Callback Mechanism

```go
// C callback signature: xev_cb_action (*)(xev_loop*, xev_completion*, int, void*)
type TimerCallback func(loop *Loop, c *Completion, result int32, userdata uintptr) CbAction

var (
    callbackRegistry sync.Map  // callbackID -> TimerCallback
    callbackCounter  uint64
    
    // Single global callback trampoline (created once)
    timerCallbackPtr uintptr
)

func init() {
    // Create the C-callable trampoline once
    timerCallbackPtr = purego.NewCallback(timerTrampoline)
}

// timerTrampoline is called from C, dispatches to registered Go callback
func timerTrampoline(loop, completion unsafe.Pointer, result int32, userdata uintptr) int32 {
    if cb, ok := callbackRegistry.Load(userdata); ok {
        action := cb.(TimerCallback)(
            (*Loop)(loop),
            (*Completion)(completion),
            result,
            userdata,
        )
        return int32(action)
    }
    return int32(Disarm)
}

// RegisterCallback stores a Go callback and returns an ID to pass as userdata
func RegisterCallback(cb TimerCallback) uintptr {
    id := uintptr(atomic.AddUint64(&callbackCounter, 1))
    callbackRegistry.Store(id, cb)
    return id
}

// UnregisterCallback removes a callback from the registry
func UnregisterCallback(id uintptr) {
    callbackRegistry.Delete(id)
}

// TimerRun starts a timer with the given callback
func TimerRun(w *Watcher, loop *Loop, c *Completion, delayMs uint64, cb TimerCallback) uintptr {
    id := RegisterCallback(cb)
    xev_timer_run(
        unsafe.Pointer(w),
        unsafe.Pointer(loop),
        unsafe.Pointer(c),
        delayMs,
        id,                // userdata = callback ID
        timerCallbackPtr,  // C function pointer
    )
    return id
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

type Timer struct {
    watcher    cxev.Watcher
    completion cxev.Completion
    callbackID uintptr  // For cleanup
}

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

    id := cxev.TimerRun(&watcher, &loop, &completion, 5000, func(l *cxev.Loop, c *cxev.Completion, result int32, userdata uintptr) cxev.CbAction {
        fmt.Println("Timer fired!")
        return cxev.Disarm
    })
    defer cxev.UnregisterCallback(id)
    
    cxev.LoopRun(&loop, cxev.RunUntilDone)
}
```

## Library Loading Strategy

### Default Search Order

1. `LIBXEV_PATH` environment variable (if set)
2. Executable directory (`./libxev.dylib`)
3. System library paths

### Platform-specific Paths

```go
func libPath() string {
    if p := os.Getenv("LIBXEV_PATH"); p != "" {
        return p
    }
    
    switch runtime.GOOS {
    case "darwin":
        return "libxev.dylib"
    case "linux":
        return "libxev.so"
    case "windows":
        return "xev.dll"
    default:
        panic("unsupported platform")
    }
}
```

### Embedding (Optional Future)

For single-binary distribution, could embed dylib using `//go:embed` and extract at runtime (similar to modernc.org/sqlite approach).

## Future Extensions

- Async watcher (`xev_async_*`)
- ThreadPool (`xev_threadpool_*`)
- Embedded dylib option
- Pre-built binaries for common platforms
