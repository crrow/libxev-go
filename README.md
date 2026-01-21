# libxev-go

Go bindings for [libxev](https://github.com/Hejsil/libxev), a high-performance event loop library written in Zig.

## Features

- **Pure Go**: No cgo required. Uses [JupiterRider/ffi](https://github.com/JupiterRider/ffi) (libffi) for FFI calls.
- **Cross-platform**: Works on macOS, Linux, and Windows.
- **Two API levels**:
  - `cxev`: Low-level FFI bindings matching libxev's C API
  - `xev`: High-level Go-idiomatic API with `time.Duration`, error handling, and channels

## Architecture

```
┌─────────────────────────────────────┐
│  Your Application                   │
├─────────────────────────────────────┤
│  xev (high-level Go API)            │  pkg/xev/
├─────────────────────────────────────┤
│  cxev (low-level FFI bindings)      │  pkg/cxev/
├─────────────────────────────────────┤
│  libffi (C calling convention)      │  github.com/jupiterrider/ffi
├─────────────────────────────────────┤
│  libxev (Zig event loop library)    │  deps/libxev/
└─────────────────────────────────────┘
```

## Quick Start

```go
package main

import (
    "fmt"
    "time"

    "github.com/crrow/libxev-go/pkg/xev"
)

func main() {
    // Create event loop
    loop, err := xev.NewLoop()
    if err != nil {
        panic(err)
    }
    defer loop.Close()

    // Create timer
    timer, err := xev.NewTimer()
    if err != nil {
        panic(err)
    }
    defer timer.Close()

    // Schedule timer with callback
    timer.RunFunc(loop, 100*time.Millisecond, func(t *xev.Timer, err error) xev.Action {
        fmt.Println("Timer fired!")
        return xev.Stop // Fire once
        // return xev.Continue // For repeating timer
    })

    // Run the event loop
    loop.Run()
}
```

## Building

### Prerequisites

- Go 1.21+
- [Zig](https://ziglang.org/) 0.13+ (for building libxev)
- [just](https://github.com/casey/just) (task runner)

### Build and Test

```bash
# Build libxev and run tests
just test

# Quick test (skip libxev rebuild)
just test-quick

# Build libxev only
just build-libxev
```

### Manual Build

```bash
# Build libxev
cd deps/libxev
zig build -Doptimize=ReleaseFast -Dshared_state=false
cd ../..

# Run tests
LIBXEV_PATH=deps/libxev/zig-out/lib/libxev.dylib go test -race ./...
```

## Package Documentation

### `pkg/cxev` - Low-level FFI Bindings

Direct bindings to libxev's C API. Use this when you need:
- Maximum control over memory and lifecycle
- Access to features not yet wrapped by `xev`
- Performance-critical code

Key concepts:
- **Opaque types**: `Loop`, `Completion`, `Watcher` are fixed-size byte arrays
- **Callbacks**: Use `RegisterCallback` + `GetTimerCallbackPtr` for C→Go callbacks
- **Manual resource management**: Must call `*Deinit` functions

### `pkg/xev` - High-level Go API

Go-idiomatic wrapper with:
- `time.Duration` instead of raw milliseconds
- `error` returns instead of error codes
- Handler interfaces and functional callbacks
- Automatic callback registration/cleanup

## How FFI Works

This library uses libffi to call C functions without cgo:

1. **Library Loading**: `ffi.Load()` loads the shared library at runtime
2. **Function Binding**: `lib.Prep()` creates call descriptors with type information
3. **Callbacks**: `ffi.Closure` creates C-callable function pointers that invoke Go code

See `pkg/cxev/callback.go` for the callback mechanism implementation.

## License

MIT License - see [LICENSE](LICENSE) for details.

Based on [libxev](https://github.com/Hejsil/libxev) by Mitchell Hashimoto.
