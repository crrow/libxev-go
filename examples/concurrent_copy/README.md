# Concurrent File Copy Benchmark

This example demonstrates and benchmarks concurrent file copy using:

1. **libxev async I/O**: Single event loop with thread pool backing
2. **Goroutine blocking I/O**: Multiple goroutines with `os.File` operations

## Key Difference

| Approach | How it works |
|----------|--------------|
| Goroutine | N goroutines, each blocking on read/write syscalls. Go runtime schedules across OS threads. |
| Xev | Single event loop dispatches I/O. Thread pool handles blocking file ops, delivers completions back to loop. |

## Usage

```bash
# Build libxev first
just build-extended

# Run benchmark (default: 50 files × 1MB)
go run ./examples/concurrent_copy

# Customize parameters
go run ./examples/concurrent_copy -files 100 -size 10485760  # 100 × 10MB files
go run ./examples/concurrent_copy -mode xev                   # Only xev
go run ./examples/concurrent_copy -mode goroutine -workers 8  # Goroutine with 8 workers
go run ./examples/concurrent_copy -keep                       # Keep generated files
```

## Parameters

| Flag | Default | Description |
|------|---------|-------------|
| `-files` | 50 | Number of files to copy |
| `-size` | 1048576 | Size of each file in bytes |
| `-mode` | both | Benchmark mode: `xev`, `goroutine`, or `both` |
| `-workers` | 0 | Max goroutine workers (0 = unlimited) |
| `-keep` | false | Keep generated files after benchmark |

## Expected Results

The relative performance depends on:

- **File count**: More files → more benefit from async I/O
- **File size**: Larger files → less overhead matters
- **Storage**: SSD vs HDD, local vs network
- **OS**: Linux io_uring vs macOS kqueue

Typical observations:
- Many small files: xev often wins (less goroutine overhead)
- Few large files: roughly equivalent (I/O bound)
- High concurrency: xev scales better (bounded thread pool vs unbounded goroutines)

## Code Structure

- `main.go` - Benchmark harness and CLI
- `xev_copy.go` - Xev-based copier using async PRead/PWrite
- `goroutine_copy.go` - Traditional goroutine-based copier
