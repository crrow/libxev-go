/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/crrow/libxev-go/pkg/cxev"
)

// File provides async file I/O operations.
//
// Unlike network I/O which can be efficiently multiplexed by the kernel,
// file I/O operations may block. libxev handles this by offloading file
// operations to a thread pool, delivering completion events back to the
// event loop.
//
// # Requirements
//
// File operations require a [Loop] created with [NewLoopWithThreadPool].
// Using a Loop without a thread pool will cause file operations to fail
// or block unexpectedly.
//
// # Opening Files
//
// Create a File with [OpenFile] or [NewFileFromFd]:
//
//	// Open a new file
//	file, err := xev.OpenFile("data.txt", os.O_RDWR|os.O_CREATE, 0644)
//	if err != nil {
//	    return err
//	}
//	defer file.Close()
//
//	// Wrap an existing file descriptor
//	file, err := xev.NewFileFromFd(int32(existingFd))
//
// # Reading and Writing
//
// File supports two modes of I/O:
//
//   - Sequential: [File.Read] and [File.Write] operate at the current file position
//   - Positional: [File.PRead] and [File.PWrite] operate at a specific offset
//
// Each operation creates its own completion, allowing multiple concurrent
// operations on the same file.
//
// Example sequential read:
//
//	buf := make([]byte, 4096)
//	file.ReadFunc(loop, buf, func(f *xev.File, data []byte, err error) xev.Action {
//	    if err != nil {
//	        return xev.Stop
//	    }
//	    process(data)
//	    return xev.Continue // Keep reading
//	})
//
// Example positional write:
//
//	file.PWriteFunc(loop, []byte("data"), 1024, func(f *xev.File, n int, err error) xev.Action {
//	    // Wrote at offset 1024
//	    return xev.Stop
//	})
type File struct {
	file cxev.File
	fd   int32
}

// FileReadHandler handles file read completions.
//
// Implement this interface for stateful read handling. For simple use cases,
// [FileReadFunc] provides a more convenient functional approach.
type FileReadHandler interface {
	// OnRead is called when a read completes.
	// data contains the bytes read (may be shorter than the buffer on EOF).
	// Return [Continue] to keep reading, or [Stop] to stop.
	OnRead(file *File, data []byte, err error) Action
}

// FileReadFunc is a function adapter for [FileReadHandler].
type FileReadFunc func(file *File, data []byte, err error) Action

// OnRead implements [FileReadHandler].
func (f FileReadFunc) OnRead(file *File, data []byte, err error) Action {
	return f(file, data, err)
}

// FileWriteHandler handles file write completions.
//
// Implement this interface for stateful write handling. For simple use cases,
// [FileWriteFunc] provides a more convenient functional approach.
type FileWriteHandler interface {
	// OnWrite is called when a write completes.
	// bytesWritten is the number of bytes successfully written.
	// Return [Continue] for chained writes, or [Stop] when done.
	OnWrite(file *File, bytesWritten int, err error) Action
}

// FileWriteFunc is a function adapter for [FileWriteHandler].
type FileWriteFunc func(file *File, bytesWritten int, err error) Action

// OnWrite implements [FileWriteHandler].
func (f FileWriteFunc) OnWrite(file *File, bytesWritten int, err error) Action {
	return f(file, bytesWritten, err)
}

// FileCloseHandler handles file close completions.
//
// Implement this interface if you need notification when a close completes.
// For simple use cases, [FileCloseFunc] provides a more convenient approach.
type FileCloseHandler interface {
	// OnClose is called when the file is fully closed.
	OnClose(file *File, err error)
}

// FileCloseFunc is a function adapter for [FileCloseHandler].
type FileCloseFunc func(file *File, err error)

// OnClose implements [FileCloseHandler].
func (f FileCloseFunc) OnClose(file *File, err error) {
	if f != nil {
		f(file, err)
	}
}

// fileOp holds per-operation state including completion and callback ID.
// Each async operation gets its own fileOp, allowing concurrent operations.
// The completion and buffer must be pinned to prevent GC from moving them while C code holds pointers.
type fileOp struct {
	file       *File
	loop       *Loop
	completion cxev.FileCompletion
	callbackID uintptr
	buf        []byte         // for read operations, to pass to callback
	pinner     runtime.Pinner // pins completion and buffer

	readHandler  FileReadHandler
	writeHandler FileWriteHandler
	closeHandler FileCloseHandler
}

var activeFileOps sync.Map

// OpenFile opens a file for async operations.
//
// The flag and perm parameters work the same as [os.OpenFile]. Common flags:
//
//   - os.O_RDONLY: Read-only
//   - os.O_WRONLY: Write-only
//   - os.O_RDWR: Read and write
//   - os.O_CREATE: Create if not exists
//   - os.O_TRUNC: Truncate on open
//   - os.O_APPEND: Append mode
//
// Returns [ErrExtLibNotLoaded] if the extended library is not available.
//
// Example:
//
//	// Open for reading
//	file, err := xev.OpenFile("data.txt", os.O_RDONLY, 0)
//
//	// Create and write
//	file, err := xev.OpenFile("output.txt", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
func OpenFile(path string, flag int, perm os.FileMode) (*File, error) {
	if !cxev.ExtLibLoaded() {
		return nil, ErrExtLibNotLoaded
	}

	f, err := os.OpenFile(path, flag, perm)
	if err != nil {
		return nil, err
	}

	// Duplicate the fd so libxev has its own copy independent of Go's GC.
	// Go's runtime sets a finalizer on *os.File that closes the fd when
	// the file becomes unreachable. By using dup, we get a separate fd
	// that libxev can use without risk of Go closing it unexpectedly.
	origFd := int(f.Fd())
	dupFd, err := syscall.Dup(origFd)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("dup fd: %w", err)
	}

	// Close the original - Go can finalize it freely now
	f.Close()

	file := &File{fd: int32(dupFd)}
	cxev.FileInitFd(&file.file, int32(dupFd))

	return file, nil
}

// NewFileFromFd creates a File from an existing file descriptor.
//
// This is useful for wrapping file descriptors obtained from other sources,
// such as pipes, inherited descriptors, or files opened by other libraries.
//
// The caller is responsible for ensuring the file descriptor is valid and
// remains valid for the lifetime of the File.
//
// Returns [ErrExtLibNotLoaded] if the extended library is not available.
func NewFileFromFd(fd int32) (*File, error) {
	if !cxev.ExtLibLoaded() {
		return nil, ErrExtLibNotLoaded
	}

	file := &File{fd: fd}
	cxev.FileInitFd(&file.file, fd)

	return file, nil
}

// Fd returns the underlying file descriptor.
func (f *File) Fd() int32 {
	return f.fd
}

// Read starts an async read at the current file position.
//
// The handler's OnRead method is called when the read completes. The data
// slice passed to the handler is a slice of buf containing the bytes read.
//
// Return [Continue] from the handler to keep reading sequentially, or [Stop]
// to stop.
func (f *File) Read(loop *Loop, buf []byte, handler FileReadHandler) error {
	op := &fileOp{
		file:        f,
		loop:        loop,
		buf:         buf,
		readHandler: handler,
	}
	op.pinner.Pin(&op.completion)
	op.pinner.Pin(&buf[0])

	op.callbackID = cxev.FileReadWithCallback(&f.file, &loop.inner, &op.completion, buf, op.readCallback)
	activeFileOps.Store(op.callbackID, op)
	return nil
}

// ReadFunc starts an async read using a callback function.
//
// This is a convenience wrapper around [File.Read] for functional-style callbacks.
func (f *File) ReadFunc(loop *Loop, buf []byte, fn func(file *File, data []byte, err error) Action) error {
	return f.Read(loop, buf, FileReadFunc(fn))
}

func (op *fileOp) readCallback(loop *cxev.Loop, c *cxev.FileCompletion, data []byte, bytesRead int32, errCode int32, userdata uintptr) cxev.CbAction {
	var err error
	if errCode != 0 {
		err = fmt.Errorf("read error: code=%d, bytesRead=%d", errCode, bytesRead)
	}

	action := op.readHandler.OnRead(op.file, data, err)
	if action == Continue {
		return cxev.Rearm
	}

	activeFileOps.Delete(op.callbackID)
	op.pinner.Unpin()
	cxev.UnregisterFileCallback(op.callbackID)
	return cxev.Disarm
}

// Write starts an async write at the current file position.
//
// The handler's OnWrite method is called when the write completes.
func (f *File) Write(loop *Loop, data []byte, handler FileWriteHandler) error {
	op := &fileOp{
		file:         f,
		loop:         loop,
		buf:          data,
		writeHandler: handler,
	}
	op.pinner.Pin(&op.completion)
	op.pinner.Pin(&data[0])

	op.callbackID = cxev.FileWriteWithCallback(&f.file, &loop.inner, &op.completion, data, op.writeCallback)
	activeFileOps.Store(op.callbackID, op)
	return nil
}

// WriteFunc starts an async write using a callback function.
//
// This is a convenience wrapper around [File.Write] for functional-style callbacks.
func (f *File) WriteFunc(loop *Loop, data []byte, fn func(file *File, bytesWritten int, err error) Action) error {
	return f.Write(loop, data, FileWriteFunc(fn))
}

func (op *fileOp) writeCallback(loop *cxev.Loop, c *cxev.FileCompletion, bytesWritten int32, errCode int32, userdata uintptr) cxev.CbAction {
	var err error
	if errCode != 0 {
		err = fmt.Errorf("write error: code=%d, bytesWritten=%d", errCode, bytesWritten)
	}

	action := op.writeHandler.OnWrite(op.file, int(bytesWritten), err)
	if action == Continue {
		return cxev.Rearm
	}

	activeFileOps.Delete(op.callbackID)
	op.pinner.Unpin()
	cxev.UnregisterFileCallback(op.callbackID)
	return cxev.Disarm
}

// PRead starts an async read at a specific offset (positional read).
//
// Unlike [File.Read], this does not modify the file position. Multiple
// PRead/PWrite operations can be interleaved without interfering with
// each other's positions.
//
// The offset is in bytes from the start of the file.
func (f *File) PRead(loop *Loop, buf []byte, offset uint64, handler FileReadHandler) error {
	op := &fileOp{
		file:        f,
		loop:        loop,
		buf:         buf,
		readHandler: handler,
	}
	op.pinner.Pin(&op.completion)
	op.pinner.Pin(&buf[0])

	op.callbackID = cxev.FilePReadWithCallback(&f.file, &loop.inner, &op.completion, buf, offset, op.readCallback)
	activeFileOps.Store(op.callbackID, op)
	return nil
}

// PReadFunc starts an async positional read using a callback function.
//
// This is a convenience wrapper around [File.PRead] for functional-style callbacks.
func (f *File) PReadFunc(loop *Loop, buf []byte, offset uint64, fn func(file *File, data []byte, err error) Action) error {
	return f.PRead(loop, buf, offset, FileReadFunc(fn))
}

// PWrite starts an async write at a specific offset (positional write).
//
// Unlike [File.Write], this does not modify the file position. Multiple
// PRead/PWrite operations can be interleaved without interfering with
// each other's positions.
//
// The offset is in bytes from the start of the file.
func (f *File) PWrite(loop *Loop, data []byte, offset uint64, handler FileWriteHandler) error {
	op := &fileOp{
		file:         f,
		loop:         loop,
		buf:          data,
		writeHandler: handler,
	}
	op.pinner.Pin(&op.completion)
	op.pinner.Pin(&data[0])

	op.callbackID = cxev.FilePWriteWithCallback(&f.file, &loop.inner, &op.completion, data, offset, op.writeCallback)
	activeFileOps.Store(op.callbackID, op)
	return nil
}

// PWriteFunc starts an async positional write using a callback function.
//
// This is a convenience wrapper around [File.PWrite] for functional-style callbacks.
func (f *File) PWriteFunc(loop *Loop, data []byte, offset uint64, fn func(file *File, bytesWritten int, err error) Action) error {
	return f.PWrite(loop, data, offset, FileWriteFunc(fn))
}

// Close starts an async close operation.
//
// The handler (if non-nil) is called when the close completes.
func (f *File) Close(loop *Loop, handler FileCloseHandler) error {
	op := &fileOp{
		file:         f,
		loop:         loop,
		closeHandler: handler,
	}
	op.pinner.Pin(&op.completion)

	op.callbackID = cxev.FileCloseWithCallback(&f.file, &loop.inner, &op.completion, func(loop *cxev.Loop, c *cxev.FileCompletion, result int32, userdata uintptr) cxev.CbAction {
		var err error
		if result != 0 {
			err = errors.New("close error")
		}
		if op.closeHandler != nil {
			op.closeHandler.OnClose(op.file, err)
		}
		activeFileOps.Delete(op.callbackID)
		op.pinner.Unpin()
		cxev.UnregisterFileCallback(op.callbackID)
		return cxev.Disarm
	})
	activeFileOps.Store(op.callbackID, op)
	return nil
}

// CloseFunc starts an async close using a callback function.
//
// This is a convenience wrapper around [File.Close] for functional-style callbacks.
func (f *File) CloseFunc(loop *Loop, fn func(file *File, err error)) error {
	return f.Close(loop, FileCloseFunc(fn))
}

// Cleanup is deprecated - callbacks are now automatically cleaned up.
// This method is kept for backward compatibility but does nothing.
func (f *File) Cleanup() {
	// No-op: callbacks are cleaned up automatically when operations complete
}
