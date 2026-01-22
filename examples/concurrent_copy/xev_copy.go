/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/crrow/libxev-go/pkg/xev"
)

// activeCopyTasks prevents copyTask from being GC'd while async operations are in flight.
// The copyTask is registered when created and removed when both files are closed.
var activeCopyTasks sync.Map

// chunkBufferPool reuses read buffers across copy tasks to reduce allocations.
// With maxConcurrent=16, we only need ~16 buffers in circulation rather than
// allocating a new buffer for each of the 50+ files being copied.
var chunkBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, chunkSize)
		return &buf
	},
}

const chunkSize = 64 * 1024 // 64KB per read/write
const maxConcurrent = 16    // Maximum concurrent file operations

// XevCopier copies multiple files concurrently using libxev async I/O.
// All operations are driven by a single event loop with thread pool backing.
type XevCopier struct {
	loop       *xev.Loop
	pending    atomic.Int32
	completed  atomic.Int32
	inFlight   atomic.Int32
	errors     []error
	waitingIdx int
	pairs      []FilePair
}

// NewXevCopier creates a copier with xev event loop.
func NewXevCopier() (*XevCopier, error) {
	loop, err := xev.NewLoopWithThreadPool()
	if err != nil {
		return nil, fmt.Errorf("create loop: %w", err)
	}
	return &XevCopier{loop: loop}, nil
}

// Close releases resources.
func (c *XevCopier) Close() {
	c.loop.Close()
}

// copyTask represents a single file copy operation.
type copyTask struct {
	copier   *XevCopier
	src      *xev.File
	dst      *xev.File
	srcPath  string
	dstPath  string
	fileSize int64
	offset   uint64
	buf      []byte
}

// CopyFiles copies all src files to dst paths concurrently.
// Returns when all copies complete.
func (c *XevCopier) CopyFiles(pairs []FilePair) error {
	c.pending.Store(int32(len(pairs)))
	c.completed.Store(0)
	c.inFlight.Store(0)
	c.errors = nil
	c.pairs = pairs
	c.waitingIdx = 0

	c.startMoreCopies()

	for c.pending.Load() > 0 {
		c.loop.RunOnce()
	}

	if len(c.errors) > 0 {
		return fmt.Errorf("%d errors occurred, first: %w", len(c.errors), c.errors[0])
	}
	return nil
}

func (c *XevCopier) startMoreCopies() {
	for c.waitingIdx < len(c.pairs) && c.inFlight.Load() < maxConcurrent {
		pair := c.pairs[c.waitingIdx]
		c.waitingIdx++
		c.inFlight.Add(1)
		if err := c.startCopy(pair.Src, pair.Dst); err != nil {
			c.errors = append(c.errors, fmt.Errorf("start copy %s: %w", pair.Src, err))
			c.inFlight.Add(-1)
			c.pending.Add(-1)
		}
	}
}

func (c *XevCopier) startCopy(srcPath, dstPath string) error {
	// Get file size
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	src, err := xev.OpenFile(srcPath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}

	dst, err := xev.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		src.Cleanup()
		return fmt.Errorf("open dst: %w", err)
	}

	// Get buffer from pool for this task
	bufPtr := chunkBufferPool.Get().(*[]byte)

	task := &copyTask{
		copier:   c,
		src:      src,
		dst:      dst,
		srcPath:  srcPath,
		dstPath:  dstPath,
		fileSize: info.Size(),
		offset:   0,
		buf:      *bufPtr,
	}

	activeCopyTasks.Store(task, struct{}{})
	return task.readNext()
}

func (t *copyTask) readNext() error {
	if t.offset >= uint64(t.fileSize) {
		// Done - close files
		return t.finish(nil)
	}

	return t.src.PReadFunc(t.copier.loop, t.buf, t.offset, t.onRead)
}

func (t *copyTask) onRead(f *xev.File, data []byte, err error) xev.Action {
	if err != nil {
		t.finish(fmt.Errorf("read at %d: %w", t.offset, err))
		return xev.Stop
	}

	if len(data) == 0 {
		t.finish(nil)
		return xev.Stop
	}

	writeData := make([]byte, len(data))
	copy(writeData, data)

	writeOffset := t.offset
	t.offset += uint64(len(data))

	dstFd := t.dst.Fd()
	if err := t.dst.PWriteFunc(t.copier.loop, writeData, writeOffset, func(f *xev.File, bytesWritten int, err error) xev.Action {
		if err != nil {
			t.finish(fmt.Errorf("write at offset %d, original_fd=%d, callback_fd=%d: %w", writeOffset, dstFd, f.Fd(), err))
			return xev.Stop
		}

		if err := t.readNext(); err != nil {
			t.finish(err)
		}
		return xev.Stop
	}); err != nil {
		t.finish(fmt.Errorf("start write at %d: %w", writeOffset, err))
		return xev.Stop
	}

	return xev.Stop
}

func (t *copyTask) finish(err error) error {
	if err != nil {
		t.copier.errors = append(t.copier.errors, fmt.Errorf("%s -> %s: %w", t.srcPath, t.dstPath, err))
	}

	var closeCount atomic.Int32

	onClose := func(f *xev.File, closeErr error) {
		f.Cleanup()
		if closeCount.Add(1) == 2 {
			// Return buffer to pool when both files are closed
			chunkBufferPool.Put(&t.buf)

			activeCopyTasks.Delete(t)
			t.copier.completed.Add(1)
			t.copier.pending.Add(-1)
			t.copier.inFlight.Add(-1)
			t.copier.startMoreCopies()
		}
	}

	t.src.CloseFunc(t.copier.loop, onClose)
	t.dst.CloseFunc(t.copier.loop, onClose)

	return err
}
