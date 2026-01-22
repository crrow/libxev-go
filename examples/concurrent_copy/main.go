/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

// concurrent_copy demonstrates and benchmarks concurrent file copy using
// libxev async I/O versus traditional goroutine-based blocking I/O.
//
// This example shows the key difference:
//   - Goroutine approach: N goroutines, each blocking on syscalls
//   - Xev approach: Single event loop, thread pool handles blocking I/O
//
// Usage:
//
//	go run . -files 100 -size 1048576 -mode both
//
// Modes:
//   - xev: Only run xev-based copy
//   - goroutine: Only run goroutine-based copy
//   - both: Run both and compare (default)
package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/crrow/libxev-go/pkg/cxev"
)

// FilePair represents a source and destination path.
type FilePair struct {
	Src string
	Dst string
}

func main() {
	numFiles := flag.Int("files", 50, "number of files to copy")
	fileSize := flag.Int64("size", 1024*1024, "size of each file in bytes")
	mode := flag.String("mode", "both", "benchmark mode: xev, goroutine, or both")
	workers := flag.Int("workers", 0, "max goroutine workers (0 = unlimited)")
	keepFiles := flag.Bool("keep", false, "keep generated files after benchmark")
	flag.Parse()

	fmt.Printf("Concurrent File Copy Benchmark\n")
	fmt.Printf("==============================\n")
	fmt.Printf("Files: %d, Size: %s each, Total: %s\n",
		*numFiles, formatBytes(*fileSize), formatBytes(*fileSize*int64(*numFiles)))
	fmt.Printf("GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))
	fmt.Printf("Mode: %s\n\n", *mode)

	// Check xev availability
	if !cxev.ExtLibLoaded() && (*mode == "xev" || *mode == "both") {
		fmt.Println("WARNING: libxev extended library not loaded")
		fmt.Println("Run 'just build-zig' to build the library")
		if *mode == "xev" {
			os.Exit(1)
		}
		*mode = "goroutine"
	}

	// Setup test files
	srcDir, dstDir, pairs, err := setupTestFiles(*numFiles, *fileSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
		os.Exit(1)
	}
	if !*keepFiles {
		defer os.RemoveAll(srcDir)
		defer os.RemoveAll(dstDir)
	} else {
		fmt.Printf("Source dir: %s\n", srcDir)
		fmt.Printf("Dest dir: %s\n\n", dstDir)
	}

	var xevDuration, goroutineDuration time.Duration

	// Run xev benchmark
	if *mode == "xev" || *mode == "both" {
		fmt.Print("Running xev copy... ")
		xevDuration, err = benchmarkXev(pairs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xev copy failed: %v\n", err)
		} else {
			throughput := float64(*fileSize*int64(*numFiles)) / xevDuration.Seconds() / 1024 / 1024
			fmt.Printf("done in %v (%.2f MB/s)\n", xevDuration.Round(time.Millisecond), throughput)
		}

		// Clean dst for next run
		if *mode == "both" {
			cleanDstDir(dstDir, pairs)
		}
	}

	// Run goroutine benchmark
	if *mode == "goroutine" || *mode == "both" {
		workerDesc := "unlimited"
		if *workers > 0 {
			workerDesc = fmt.Sprintf("%d workers", *workers)
		}
		fmt.Printf("Running goroutine copy (%s)... ", workerDesc)
		goroutineDuration, err = benchmarkGoroutine(pairs, *workers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "goroutine copy failed: %v\n", err)
		} else {
			throughput := float64(*fileSize*int64(*numFiles)) / goroutineDuration.Seconds() / 1024 / 1024
			fmt.Printf("done in %v (%.2f MB/s)\n", goroutineDuration.Round(time.Millisecond), throughput)
		}
	}

	// Print comparison
	if *mode == "both" && xevDuration > 0 && goroutineDuration > 0 {
		fmt.Printf("\nComparison:\n")
		if xevDuration < goroutineDuration {
			speedup := float64(goroutineDuration) / float64(xevDuration)
			fmt.Printf("  xev is %.2fx faster\n", speedup)
		} else {
			speedup := float64(xevDuration) / float64(goroutineDuration)
			fmt.Printf("  goroutine is %.2fx faster\n", speedup)
		}
	}

	// Verify copied files
	if err := verifyFiles(pairs); err != nil {
		fmt.Fprintf(os.Stderr, "verification failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nVerification: OK")
}

func setupTestFiles(numFiles int, fileSize int64) (srcDir, dstDir string, pairs []FilePair, err error) {
	srcDir, err = os.MkdirTemp("", "copy_bench_src_*")
	if err != nil {
		return "", "", nil, err
	}

	dstDir, err = os.MkdirTemp("", "copy_bench_dst_*")
	if err != nil {
		os.RemoveAll(srcDir)
		return "", "", nil, err
	}

	pairs = make([]FilePair, numFiles)
	data := make([]byte, fileSize)

	for i := 0; i < numFiles; i++ {
		// Generate random data for each file
		if _, err := rand.Read(data); err != nil {
			os.RemoveAll(srcDir)
			os.RemoveAll(dstDir)
			return "", "", nil, fmt.Errorf("generate random data: %w", err)
		}

		srcPath := filepath.Join(srcDir, fmt.Sprintf("file_%04d.bin", i))
		dstPath := filepath.Join(dstDir, fmt.Sprintf("file_%04d.bin", i))

		if err := os.WriteFile(srcPath, data, 0644); err != nil {
			os.RemoveAll(srcDir)
			os.RemoveAll(dstDir)
			return "", "", nil, fmt.Errorf("write source file: %w", err)
		}

		pairs[i] = FilePair{Src: srcPath, Dst: dstPath}
	}

	return srcDir, dstDir, pairs, nil
}

func cleanDstDir(dstDir string, pairs []FilePair) {
	for _, pair := range pairs {
		os.Remove(pair.Dst)
	}
}

func benchmarkXev(pairs []FilePair) (time.Duration, error) {
	copier, err := NewXevCopier()
	if err != nil {
		return 0, err
	}
	defer copier.Close()

	start := time.Now()
	err = copier.CopyFiles(pairs)
	return time.Since(start), err
}

func benchmarkGoroutine(pairs []FilePair, maxWorkers int) (time.Duration, error) {
	copier := NewGoroutineCopier(maxWorkers)

	start := time.Now()
	err := copier.CopyFiles(pairs)
	return time.Since(start), err
}

func verifyFiles(pairs []FilePair) error {
	for _, pair := range pairs {
		srcInfo, err := os.Stat(pair.Src)
		if err != nil {
			return fmt.Errorf("stat src %s: %w", pair.Src, err)
		}

		dstInfo, err := os.Stat(pair.Dst)
		if err != nil {
			return fmt.Errorf("stat dst %s: %w", pair.Dst, err)
		}

		if srcInfo.Size() != dstInfo.Size() {
			return fmt.Errorf("size mismatch: %s (%d) vs %s (%d)",
				pair.Src, srcInfo.Size(), pair.Dst, dstInfo.Size())
		}
	}
	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
