# libxev-go

> **🚧 Work in Progress**: This library is under active development. APIs may change.

Go bindings for [libxev](https://github.com/mitchellh/libxev), a high-performance event loop library written in Zig.

## Features

- **Pure Go**: No cgo required. Uses [JupiterRider/ffi](https://github.com/JupiterRider/ffi) (libffi) for FFI calls.
- **Cross-platform**: Supports macOS (kqueue), Linux (io_uring), and Windows (IOCP).
- **Async I/O**: High-performance file operations with thread pool backing for blocking operations.
- **Two API levels**:
  - `cxev`: Low-level FFI bindings matching libxev's C API
  - `xev`: High-level Go-idiomatic API with `time.Duration`, error handling, and callbacks

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
    "os"

    "github.com/crrow/libxev-go/pkg/xev"
)

func main() {
    // Create event loop
    loop, err := xev.NewLoop()
    if err != nil {
        panic(err)
    }
    defer loop.Close()

    // Open file for async read
    file, err := xev.OpenFile("example.txt", os.O_RDONLY, 0)
    if err != nil {
        panic(err)
    }
    defer file.Close()

    // Prepare buffer for reading
    buf := make([]byte, 1024)

    // Async read with callback
    file.ReadFunc(loop, buf, 0, func(f *xev.File, buf []byte, n int, err error) xev.Action {
        if err != nil {
            fmt.Printf("Read error: %v\n", err)
            return xev.Disarm
        }
        fmt.Printf("Read %d bytes: %s\n", n, string(buf[:n]))
        return xev.Disarm
    })

    // Run the event loop
    loop.Run()
}
```

## Examples

See [examples/concurrent_copy](examples/concurrent_copy) for a complete benchmark comparing libxev async I/O vs goroutine-based blocking I/O for concurrent file operations.

```bash
just build-extended
just example-concurrent-copy
```

## Building

### Prerequisites

- Go 1.25
- [Zig](https://ziglang.org/) 0.15 (for building libxev)

## License

MIT License - see [LICENSE](LICENSE) for details.

Based on [libxev](https://github.com/Hejsil/libxev) by Mitchell Hashimoto.
