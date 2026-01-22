/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/crrow/libxev-go/pkg/xev"
)

const chunkSize = 64 * 1024 // 64KB per read/write

// XevCopier copies multiple files concurrently using libxev async I/O.
// All operations are driven by a single event loop with thread pool backing.
type XevCopier struct {
	loop      *xev.Loop
	pending   atomic.Int32
	completed atomic.Int32
	errors    []error
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
	c.errors = nil

	for _, pair := range pairs {
		if err := c.startCopy(pair.Src, pair.Dst); err != nil {
			c.errors = append(c.errors, fmt.Errorf("start copy %s: %w", pair.Src, err))
			c.pending.Add(-1)
		}
	}

	// Run event loop until all copies complete
	for c.pending.Load() > 0 {
		c.loop.RunOnce()
	}

	if len(c.errors) > 0 {
		return fmt.Errorf("%d errors occurred, first: %w", len(c.errors), c.errors[0])
	}
	return nil
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

	task := &copyTask{
		copier:   c,
		src:      src,
		dst:      dst,
		srcPath:  srcPath,
		dstPath:  dstPath,
		fileSize: info.Size(),
		offset:   0,
		buf:      make([]byte, chunkSize),
	}

	// Start reading
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
		// EOF
		t.finish(nil)
		return xev.Stop
	}

	// Write the data we just read
	writeData := make([]byte, len(data))
	copy(writeData, data)

	writeOffset := t.offset
	t.offset += uint64(len(data))

	if err := t.dst.PWriteFunc(t.copier.loop, writeData, writeOffset, t.onWrite); err != nil {
		t.finish(fmt.Errorf("start write: %w", err))
		return xev.Stop
	}

	return xev.Stop
}

func (t *copyTask) onWrite(f *xev.File, bytesWritten int, err error) xev.Action {
	if err != nil {
		t.finish(fmt.Errorf("write: %w", err))
		return xev.Stop
	}

	// Continue reading
	if err := t.readNext(); err != nil {
		t.finish(err)
	}
	return xev.Stop
}

func (t *copyTask) finish(err error) error {
	if err != nil {
		t.copier.errors = append(t.copier.errors, fmt.Errorf("%s -> %s: %w", t.srcPath, t.dstPath, err))
	}

	// Close source
	t.src.CloseFunc(t.copier.loop, func(f *xev.File, closeErr error) {
		f.Cleanup()
	})

	// Close destination
	t.dst.CloseFunc(t.copier.loop, func(f *xev.File, closeErr error) {
		f.Cleanup()
		t.copier.completed.Add(1)
		t.copier.pending.Add(-1)
	})

	return err
}
