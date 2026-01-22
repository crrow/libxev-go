# Concurrent File Copy Benchmark

Benchmark comparing libxev async I/O vs traditional goroutine blocking I/O for concurrent file copy.

## Quick Start

```bash
# Build library
just build-extended

# Run interactive TUI (recommended)
just example-concurrent-copy

# Run all scenarios
just example-concurrent-copy-bench
```

## Results (macOS M3 Max, SSD)

| Scenario | xev | goroutine | Winner |
|----------|-----|-----------|--------|
| 1000 × 4KB | 46 MB/s | 38 MB/s | xev 1.20x |
| 200 × 64KB | 607 MB/s | 319 MB/s | xev 1.90x |
| 100 × 256KB | 1319 MB/s | 622 MB/s | xev 2.12x |
| 50 × 1MB | 2612 MB/s | 981 MB/s | **xev 2.66x** ⭐ |
| 20 × 5MB | 3228 MB/s | 1302 MB/s | xev 2.48x |
| 10 × 10MB | 3340 MB/s | 2460 MB/s | xev 1.36x |
| 5 × 50MB | 2229 MB/s | 4693 MB/s | **goroutine 2.11x** ⚡ |

**Key Finding**: xev excels at many small files, goroutine wins at large sequential I/O. Crossover around 10-50MB.

## Implementation

- **xev**: Event loop + thread pool, async PRead/PWrite, buffer pooling
- **goroutine**: Unlimited goroutines, `io.CopyBuffer`, buffer pooling

Both use `sync.Pool` for fair comparison.
