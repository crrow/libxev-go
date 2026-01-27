/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestOsFileFinalizerBehavior verifies Go's GC finalizer behavior with *os.File.
//
// Key hypothesis:
// - Holding *os.File in a struct is NOT sufficient to prevent finalization
// - GC may finalize if it determines the object won't be "used" anymore
// - syscall.Dup provides isolation from this behavior
func TestOsFileFinalizerBehavior(t *testing.T) {
	t.Run("finalizer_runs_when_only_fd_escapes", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "gc_test_*")
		if err != nil {
			t.Fatal(err)
		}
		tmpPath := tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(tmpPath)

		var fd int
		var finalizerRan atomic.Bool

		func() {
			f, err := os.OpenFile(tmpPath, os.O_RDWR, 0644)
			if err != nil {
				t.Fatal(err)
			}

			fd = int(f.Fd())

			runtime.SetFinalizer(f, func(file *os.File) {
				finalizerRan.Store(true)
			})
		}()

		for i := 0; i < 20; i++ {
			runtime.GC()
			time.Sleep(5 * time.Millisecond)
		}

		if finalizerRan.Load() {
			t.Log("CONFIRMED: finalizer ran even though fd was extracted - this is the root cause")

			var stat syscall.Stat_t
			err := syscall.Fstat(fd, &stat)
			if err != nil {
				t.Logf("fd is now invalid (EBADF): %v", err)
			}
		} else {
			t.Log("finalizer did not run in this iteration")
		}
	})

	t.Run("concurrent_gc_pressure_with_callbacks", func(t *testing.T) {
		t.Skip("Skipping stress test - designed to demonstrate GC crash, not for CI")
		if !cxevLoaded() {
			t.Skip("libxev not loaded")
		}

		const numFiles = 50
		const iterations = 3

		for iter := 0; iter < iterations; iter++ {
			loop, err := NewLoopWithThreadPool()
			if err != nil {
				t.Fatal(err)
			}

			var wg sync.WaitGroup
			var failures atomic.Int32

			// Create temp files
			tmpDir, err := os.MkdirTemp("", "gc_pressure_*")
			if err != nil {
				t.Fatal(err)
			}

			// Aggressive GC in background
			stopGC := make(chan struct{})
			go func() {
				for {
					select {
					case <-stopGC:
						return
					default:
						runtime.GC()
						time.Sleep(1 * time.Millisecond)
					}
				}
			}()

			for i := 0; i < numFiles; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()

					path := tmpDir + "/file_" + string(rune('0'+idx%10)) + string(rune('0'+idx/10))
					f, err := OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
					if err != nil {
						failures.Add(1)
						return
					}

					buf := []byte("test data for gc pressure test")
					done := make(chan struct{})

					f.WriteFunc(loop, buf, func(file *File, n int, err error) Action {
						if err != nil {
							failures.Add(1)
						}
						close(done)
						return Stop
					})

					<-done
					f.CloseFunc(loop, nil)
				}(i)
			}

			// Run loop until all operations complete
			allDone := make(chan struct{})
			go func() {
				wg.Wait()
				close(allDone)
			}()

			timeout := time.After(5 * time.Second)
		runLoop:
			for {
				select {
				case <-timeout:
					t.Error("timeout waiting for operations")
					break runLoop
				case <-allDone:
					break runLoop
				default:
					loop.RunOnce()
				}
			}

			close(stopGC)
			loop.Close()
			os.RemoveAll(tmpDir)

			if f := failures.Load(); f > 0 {
				t.Logf("Iteration %d: %d failures (possible GC-related fd closure)", iter, f)
			}
		}
	})

	t.Run("dup_provides_isolation", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "dup_test_*")
		if err != nil {
			t.Fatal(err)
		}
		tmpPath := tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(tmpPath)

		f, err := os.OpenFile(tmpPath, os.O_RDWR, 0644)
		if err != nil {
			t.Fatal(err)
		}

		// Dup the fd
		origFd := int(f.Fd())
		dupFd, err := syscall.Dup(origFd)
		if err != nil {
			t.Fatal(err)
		}

		// Close the original os.File - this triggers finalizer immediately
		f.Close()

		// Force GC
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
		runtime.GC()

		// The dup'd fd should still be valid
		var stat syscall.Stat_t
		err = syscall.Fstat(dupFd, &stat)
		if err != nil {
			t.Errorf("dup'd fd should be valid after original closed: %v", err)
		} else {
			t.Log("dup'd fd remains valid after original os.File closed - isolation works")
		}

		// Clean up
		syscall.Close(dupFd)
	})
}

func cxevLoaded() bool {
	defer func() { recover() }()
	_, err := NewLoop()
	return err == nil
}
