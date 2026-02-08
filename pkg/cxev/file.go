/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/jupiterrider/ffi"
)

// File-related sizes from the extended API.
const (
	SizeofFile           = 16  // xev_file: file descriptor storage
	SizeofFileCompletion = 320 // Extended completion: 256 (xev.Completion) + 8 (c_callback) + 8 (c_userdata) + padding
)

// File represents a file handle for async I/O.
type File [SizeofFile]byte

// FileCompletion is an extended completion for File operations.
// It includes extra space for the C callback pointer.
type FileCompletion [SizeofFileCompletion]byte

// FFI function descriptors for File operations.
var (
	fnFileInitFd ffi.Fun
	fnFileFd     ffi.Fun
	fnFileRead   ffi.Fun
	fnFileWrite  ffi.Fun
	fnFilePRead  ffi.Fun
	fnFilePWrite ffi.Fun
	fnFileClose  ffi.Fun
)

func registerFileFunctions() error {
	var err error

	// void xev_file_init_fd(xev_file* file, int fd)
	fnFileInitFd, err = libExt.Prep("xev_file_init_fd", &ffi.TypeVoid, &ffi.TypePointer, &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// int xev_file_fd(xev_file* file)
	fnFileFd, err = libExt.Prep("xev_file_fd", &ffi.TypeSint32, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_file_read(file, loop, completion, buf, buf_len, callback, userdata)
	fnFileRead, err = libExt.Prep("xev_file_read", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_file_write(file, loop, completion, buf, buf_len, callback, userdata)
	fnFileWrite, err = libExt.Prep("xev_file_write", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_file_pread(file, loop, completion, buf, buf_len, offset, callback, userdata)
	fnFilePRead, err = libExt.Prep("xev_file_pread", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeUint64, &ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_file_pwrite(file, loop, completion, buf, buf_len, offset, callback, userdata)
	fnFilePWrite, err = libExt.Prep("xev_file_pwrite", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeUint64, &ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_file_close(file, loop, completion, callback, userdata)
	fnFileClose, err = libExt.Prep("xev_file_close", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	return nil
}

// FileInitFd initializes a File from an existing file descriptor.
func FileInitFd(file *File, fd int32) {
	ptr := unsafe.Pointer(file)
	fnFileInitFd.Call(nil, &ptr, &fd)
}

// FileFd returns the file descriptor of a File.
func FileFd(file *File) int32 {
	var ret ffi.Arg
	ptr := unsafe.Pointer(file)
	fnFileFd.Call(&ret, &ptr)
	return int32(ret)
}

// FileError represents an error from File operations.
type FileError int32

func (e FileError) Error() string {
	return "file error: " + string(rune(e))
}

// File Callback types - these have the same signature as TCP callbacks.

// FileReadCallback is called when data is read.
// buf contains the data read (up to bytesRead bytes).
// If err != 0, an error occurred and bytesRead will be -1.
type FileReadCallback func(loop *Loop, c *FileCompletion, buf []byte, bytesRead int32, err int32, userdata uintptr) CbAction

// FileWriteCallback is called when data is written.
// If err != 0, an error occurred and bytesWritten will be -1.
type FileWriteCallback func(loop *Loop, c *FileCompletion, bytesWritten int32, err int32, userdata uintptr) CbAction

// FileCallback is called for simple file operations (close).
// result is 0 on success, or an error code on failure.
type FileCallback func(loop *Loop, c *FileCompletion, result int32, userdata uintptr) CbAction

// File callback registry - separate from TCP to allow independent cleanup.
var (
	fileCallbackRegistry      sync.Map
	fileReadCallbackRegistry  sync.Map
	fileWriteCallbackRegistry sync.Map
	fileCallbackCounter       uint64
)

// File callback closure state.
// The callback signatures match TCP's, so we can reuse the same CIF structures,
// but we create separate closures to keep the registries independent.
var (
	fileClosureInit sync.Once

	fileCallbackPtr      uintptr
	fileReadCallbackPtr  uintptr
	fileWriteCallbackPtr uintptr

	fileClosure      *ffi.Closure
	fileClosureCode  unsafe.Pointer
	fileReadClosure  *ffi.Closure
	fileReadCode     unsafe.Pointer
	fileWriteClosure *ffi.Closure
	fileWriteCode    unsafe.Pointer

	fileCif      ffi.Cif
	fileReadCif  ffi.Cif
	fileWriteCif ffi.Cif
)

func initFileClosures() {
	fileClosureInit.Do(func() {
		// File simple callback: (loop*, completion*, result int32, userdata*) -> int32
		fileClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &fileClosureCode)
		if status := ffi.PrepCif(&fileCif, ffi.DefaultAbi, 4,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare File callback CIF")
		}
		goCallback := ffi.NewCallback(fileTrampoline)
		if status := ffi.PrepClosureLoc(fileClosure, &fileCif, goCallback, nil, fileClosureCode); status != ffi.OK {
			panic("failed to prepare File closure")
		}
		fileCallbackPtr = uintptr(fileClosureCode)

		// File read callback: (loop*, completion*, buf*, bytes_read int32, err int32, userdata*) -> int32
		fileReadClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &fileReadCode)
		if status := ffi.PrepCif(&fileReadCif, ffi.DefaultAbi, 6,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare File read callback CIF")
		}
		goReadCallback := ffi.NewCallback(fileReadTrampoline)
		if status := ffi.PrepClosureLoc(fileReadClosure, &fileReadCif, goReadCallback, nil, fileReadCode); status != ffi.OK {
			panic("failed to prepare File read closure")
		}
		fileReadCallbackPtr = uintptr(fileReadCode)

		// File write callback: (loop*, completion*, bytes_written int32, err int32, userdata*) -> int32
		fileWriteClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &fileWriteCode)
		if status := ffi.PrepCif(&fileWriteCif, ffi.DefaultAbi, 5,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare File write callback CIF")
		}
		goWriteCallback := ffi.NewCallback(fileWriteTrampoline)
		if status := ffi.PrepClosureLoc(fileWriteClosure, &fileWriteCif, goWriteCallback, nil, fileWriteCode); status != ffi.OK {
			panic("failed to prepare File write closure")
		}
		fileWriteCallbackPtr = uintptr(fileWriteCode)
	})
}

func fileTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 4)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	result := *(*int32)(arguments[2])
	userdata := *(*uintptr)(arguments[3])

	action := int32(Disarm)
	if cb, ok := fileCallbackRegistry.Load(userdata); ok {
		action = int32(cb.(FileCallback)(
			(*Loop)(loop),
			(*FileCompletion)(completion),
			result,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

// fileReadContext holds the buffer pointer and length for read callbacks.
type fileReadContext struct {
	cb  FileReadCallback
	buf []byte
}

func fileReadTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 6)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	_ = *(*unsafe.Pointer)(arguments[2]) // buf ptr (we use our stored slice)
	bytesRead := *(*int32)(arguments[3])
	errCode := *(*int32)(arguments[4])
	userdata := *(*uintptr)(arguments[5])

	action := int32(Disarm)
	if ctx, ok := fileReadCallbackRegistry.Load(userdata); ok {
		readCtx := ctx.(fileReadContext)
		var buf []byte
		if bytesRead > 0 {
			buf = readCtx.buf[:bytesRead]
		}
		action = int32(readCtx.cb(
			(*Loop)(loop),
			(*FileCompletion)(completion),
			buf,
			bytesRead,
			errCode,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

// fileWriteContext holds the buffer for write callbacks to prevent GC.
type fileWriteContext struct {
	cb  FileWriteCallback
	buf []byte
}

func fileWriteTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 5)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	bytesWritten := *(*int32)(arguments[2])
	errCode := *(*int32)(arguments[3])
	userdata := *(*uintptr)(arguments[4])

	action := int32(Disarm)
	if ctx, ok := fileWriteCallbackRegistry.Load(userdata); ok {
		writeCtx := ctx.(fileWriteContext)
		action = int32(writeCtx.cb(
			(*Loop)(loop),
			(*FileCompletion)(completion),
			bytesWritten,
			errCode,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

// RegisterFileCallback registers a File callback and returns its unique ID.
func RegisterFileCallback(cb FileCallback) uintptr {
	id := uintptr(atomic.AddUint64(&fileCallbackCounter, 1))
	fileCallbackRegistry.Store(id, cb)
	return id
}

// RegisterFileReadCallback registers a File read callback with its buffer.
func RegisterFileReadCallback(cb FileReadCallback, buf []byte) uintptr {
	id := uintptr(atomic.AddUint64(&fileCallbackCounter, 1))
	fileReadCallbackRegistry.Store(id, fileReadContext{cb: cb, buf: buf})
	return id
}

// RegisterFileWriteCallback registers a File write callback with its buffer.
func RegisterFileWriteCallback(cb FileWriteCallback, buf []byte) uintptr {
	id := uintptr(atomic.AddUint64(&fileCallbackCounter, 1))
	fileWriteCallbackRegistry.Store(id, fileWriteContext{cb: cb, buf: buf})
	return id
}

// UnregisterFileCallback removes a File callback from the registry.
func UnregisterFileCallback(id uintptr) {
	fileCallbackRegistry.Delete(id)
	fileReadCallbackRegistry.Delete(id)
	fileWriteCallbackRegistry.Delete(id)
}

// GetFileCallbackPtr returns the C function pointer for File callbacks.
func GetFileCallbackPtr() uintptr {
	initFileClosures()
	return fileCallbackPtr
}

// GetFileReadCallbackPtr returns the C function pointer for read callbacks.
func GetFileReadCallbackPtr() uintptr {
	initFileClosures()
	return fileReadCallbackPtr
}

// GetFileWriteCallbackPtr returns the C function pointer for write callbacks.
func GetFileWriteCallbackPtr() uintptr {
	initFileClosures()
	return fileWriteCallbackPtr
}

// FileRead starts reading from a file at the current position.
func FileRead(file *File, loop *Loop, c *FileCompletion, buf []byte, userdata, cb uintptr) {
	filePtr := unsafe.Pointer(file)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	bufPtr := bufferPointer(buf)
	bufLen := uint64(len(buf))
	fnFileRead.Call(nil, &filePtr, &loopPtr, &cPtr, &bufPtr, &bufLen, &cb, &userdata)
}

// FileReadWithCallback is a convenience function that registers the callback and starts reading.
func FileReadWithCallback(file *File, loop *Loop, c *FileCompletion, buf []byte, cb FileReadCallback) uintptr {
	initFileClosures()
	id := RegisterFileReadCallback(cb, buf)
	FileRead(file, loop, c, buf, id, fileReadCallbackPtr)
	return id
}

// FileWrite starts writing to a file at the current position.
func FileWrite(file *File, loop *Loop, c *FileCompletion, buf []byte, userdata, cb uintptr) {
	filePtr := unsafe.Pointer(file)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	bufPtr := bufferPointer(buf)
	bufLen := uint64(len(buf))
	fnFileWrite.Call(nil, &filePtr, &loopPtr, &cPtr, &bufPtr, &bufLen, &cb, &userdata)
}

// FileWriteWithCallback is a convenience function that registers the callback and starts writing.
func FileWriteWithCallback(file *File, loop *Loop, c *FileCompletion, buf []byte, cb FileWriteCallback) uintptr {
	initFileClosures()
	id := RegisterFileWriteCallback(cb, buf)
	FileWrite(file, loop, c, buf, id, fileWriteCallbackPtr)
	return id
}

// FilePRead starts reading from a file at a specific offset.
func FilePRead(file *File, loop *Loop, c *FileCompletion, buf []byte, offset uint64, userdata, cb uintptr) {
	filePtr := unsafe.Pointer(file)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	bufPtr := bufferPointer(buf)
	bufLen := uint64(len(buf))
	fnFilePRead.Call(nil, &filePtr, &loopPtr, &cPtr, &bufPtr, &bufLen, &offset, &cb, &userdata)
}

// FilePReadWithCallback is a convenience function for positional read.
func FilePReadWithCallback(file *File, loop *Loop, c *FileCompletion, buf []byte, offset uint64, cb FileReadCallback) uintptr {
	initFileClosures()
	id := RegisterFileReadCallback(cb, buf)
	FilePRead(file, loop, c, buf, offset, id, fileReadCallbackPtr)
	return id
}

// FilePWrite starts writing to a file at a specific offset.
func FilePWrite(file *File, loop *Loop, c *FileCompletion, buf []byte, offset uint64, userdata, cb uintptr) {
	filePtr := unsafe.Pointer(file)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	bufPtr := bufferPointer(buf)
	bufLen := uint64(len(buf))
	fnFilePWrite.Call(nil, &filePtr, &loopPtr, &cPtr, &bufPtr, &bufLen, &offset, &cb, &userdata)
}

// FilePWriteWithCallback is a convenience function for positional write.
func FilePWriteWithCallback(file *File, loop *Loop, c *FileCompletion, buf []byte, offset uint64, cb FileWriteCallback) uintptr {
	initFileClosures()
	id := RegisterFileWriteCallback(cb, buf)
	FilePWrite(file, loop, c, buf, offset, id, fileWriteCallbackPtr)
	return id
}

// FileClose starts closing a file.
func FileClose(file *File, loop *Loop, c *FileCompletion, userdata, cb uintptr) {
	filePtr := unsafe.Pointer(file)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	fnFileClose.Call(nil, &filePtr, &loopPtr, &cPtr, &cb, &userdata)
}

// FileCloseWithCallback is a convenience function that registers the callback and starts closing.
func FileCloseWithCallback(file *File, loop *Loop, c *FileCompletion, cb FileCallback) uintptr {
	initFileClosures()
	id := RegisterFileCallback(cb)
	FileClose(file, loop, c, id, fileCallbackPtr)
	return id
}
