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

// UDP-related sizes from the extended API.
const (
	SizeofUDP           = 16  // xev_udp: socket storage
	SizeofUDPState      = 256 // xev_udp_state: operation state
	SizeofUDPCompletion = 320 // Extended completion with callback pointer
)

// UDP represents a UDP socket.
type UDP [SizeofUDP]byte

// UDPState holds state for UDP operations.
// UDP operations require extra state for address handling.
type UDPState [SizeofUDPState]byte

// UDPCompletion is an extended completion for UDP operations.
type UDPCompletion [SizeofUDPCompletion]byte

// FFI function descriptors for UDP operations.
var (
	fnUDPInit        ffi.Fun
	fnUDPInitFd      ffi.Fun
	fnUDPFd          ffi.Fun
	fnUDPBind        ffi.Fun
	fnUDPGetsockname ffi.Fun
	fnUDPRead        ffi.Fun
	fnUDPWrite       ffi.Fun
	fnUDPClose       ffi.Fun
)

func registerUDPFunctions() error {
	var err error

	// int xev_udp_init(xev_udp* udp, int family)
	fnUDPInit, err = libExt.Prep("xev_udp_init", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// void xev_udp_init_fd(xev_udp* udp, int fd)
	fnUDPInitFd, err = libExt.Prep("xev_udp_init_fd", &ffi.TypeVoid, &ffi.TypePointer, &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// int xev_udp_fd(xev_udp* udp)
	fnUDPFd, err = libExt.Prep("xev_udp_fd", &ffi.TypeSint32, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// int xev_udp_bind(xev_udp* udp, xev_sockaddr* addr)
	fnUDPBind, err = libExt.Prep("xev_udp_bind", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// int xev_udp_getsockname(xev_udp* udp, xev_sockaddr* addr)
	fnUDPGetsockname, err = libExt.Prep("xev_udp_getsockname", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_udp_read(xev_udp*, xev_loop*, xev_completion*, xev_udp_state*, buf, buf_len, void* userdata, callback)
	fnUDPRead, err = libExt.Prep("xev_udp_read", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer,
		&ffi.TypePointer, &ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_udp_write(xev_udp*, xev_loop*, xev_completion*, xev_udp_state*, xev_sockaddr*, buf, buf_len, void* userdata, callback)
	fnUDPWrite, err = libExt.Prep("xev_udp_write", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_udp_close(xev_udp*, xev_loop*, xev_completion*, void* userdata, callback)
	fnUDPClose, err = libExt.Prep("xev_udp_close", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	return nil
}

// UDPInit initializes a UDP socket with the given address family.
func UDPInit(udp *UDP, family int32) error {
	if loadErr != nil {
		return loadErr
	}
	var ret ffi.Arg
	ptr := unsafe.Pointer(udp)
	fnUDPInit.Call(&ret, &ptr, &family)
	if int32(ret) != 0 {
		return UDPError(int32(ret))
	}
	return nil
}

// UDPInitFd initializes a UDP socket from an existing file descriptor.
func UDPInitFd(udp *UDP, fd int32) {
	ptr := unsafe.Pointer(udp)
	fnUDPInitFd.Call(nil, &ptr, &fd)
}

// UDPFd returns the file descriptor of a UDP socket.
func UDPFd(udp *UDP) int32 {
	var ret ffi.Arg
	ptr := unsafe.Pointer(udp)
	fnUDPFd.Call(&ret, &ptr)
	return int32(ret)
}

// UDPBind binds a UDP socket to an address.
func UDPBind(udp *UDP, addr *Sockaddr) error {
	var ret ffi.Arg
	udpPtr := unsafe.Pointer(udp)
	addrPtr := unsafe.Pointer(addr)
	fnUDPBind.Call(&ret, &udpPtr, &addrPtr)
	if int32(ret) != 0 {
		return UDPError(int32(ret))
	}
	return nil
}

// UDPGetsockname gets the local address of a bound UDP socket.
func UDPGetsockname(udp *UDP, addr *Sockaddr) error {
	var ret ffi.Arg
	udpPtr := unsafe.Pointer(udp)
	addrPtr := unsafe.Pointer(addr)
	fnUDPGetsockname.Call(&ret, &udpPtr, &addrPtr)
	if int32(ret) != 0 {
		return UDPError(int32(ret))
	}
	return nil
}

// UDPError represents an error from UDP operations.
type UDPError int32

func (e UDPError) Error() string {
	return "udp error: " + string(rune(e))
}

// UDP Callback types

// UDPReadCallback is called when data is received.
// It includes the remote address from which data was received.
type UDPReadCallback func(loop *Loop, c *UDPCompletion, remoteAddr *Sockaddr, buf []byte, bytesRead int32, err int32, userdata uintptr) CbAction

// UDPWriteCallback is called when data is sent.
type UDPWriteCallback func(loop *Loop, c *UDPCompletion, bytesWritten int32, err int32, userdata uintptr) CbAction

// UDPCallback is called for simple UDP operations (close).
type UDPCallback func(loop *Loop, c *UDPCompletion, result int32, userdata uintptr) CbAction

// UDP callback registry
var (
	udpReadCallbackRegistry  sync.Map
	udpWriteCallbackRegistry sync.Map
	udpCallbackRegistry      sync.Map
	udpCallbackCounter       uint64
)

// UDP callback closure state
var (
	udpClosureInit sync.Once

	udpReadCallbackPtr  uintptr
	udpWriteCallbackPtr uintptr
	udpCallbackPtr      uintptr

	udpReadClosure  *ffi.Closure
	udpReadCode     unsafe.Pointer
	udpWriteClosure *ffi.Closure
	udpWriteCode    unsafe.Pointer
	udpClosure      *ffi.Closure
	udpClosureCode  unsafe.Pointer

	udpReadCif  ffi.Cif
	udpWriteCif ffi.Cif
	udpCif      ffi.Cif
)

func initUDPClosures() {
	udpClosureInit.Do(func() {
		// UDP read callback: (loop*, completion*, remote_addr*, buf*, bytes_read int32, err int32, userdata*) -> int32
		udpReadClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &udpReadCode)
		if status := ffi.PrepCif(&udpReadCif, ffi.DefaultAbi, 7,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer,
			&ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare UDP read callback CIF")
		}
		goReadCallback := ffi.NewCallback(udpReadTrampoline)
		if status := ffi.PrepClosureLoc(udpReadClosure, &udpReadCif, goReadCallback, nil, udpReadCode); status != ffi.OK {
			panic("failed to prepare UDP read closure")
		}
		udpReadCallbackPtr = uintptr(udpReadCode)

		// UDP write callback: (loop*, completion*, bytes_written int32, err int32, userdata*) -> int32
		udpWriteClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &udpWriteCode)
		if status := ffi.PrepCif(&udpWriteCif, ffi.DefaultAbi, 5,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare UDP write callback CIF")
		}
		goWriteCallback := ffi.NewCallback(udpWriteTrampoline)
		if status := ffi.PrepClosureLoc(udpWriteClosure, &udpWriteCif, goWriteCallback, nil, udpWriteCode); status != ffi.OK {
			panic("failed to prepare UDP write closure")
		}
		udpWriteCallbackPtr = uintptr(udpWriteCode)

		// UDP simple callback: (loop*, completion*, result int32, userdata*) -> int32
		udpClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &udpClosureCode)
		if status := ffi.PrepCif(&udpCif, ffi.DefaultAbi, 4,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare UDP callback CIF")
		}
		goCallback := ffi.NewCallback(udpTrampoline)
		if status := ffi.PrepClosureLoc(udpClosure, &udpCif, goCallback, nil, udpClosureCode); status != ffi.OK {
			panic("failed to prepare UDP closure")
		}
		udpCallbackPtr = uintptr(udpClosureCode)
	})
}

// udpReadContext holds the buffer pointer and length for read callbacks.
type udpReadContext struct {
	cb  UDPReadCallback
	buf []byte
}

func udpReadTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 7)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	remoteAddr := *(*unsafe.Pointer)(arguments[2])
	_ = *(*unsafe.Pointer)(arguments[3]) // buf ptr (we use our stored slice)
	bytesRead := *(*int32)(arguments[4])
	errCode := *(*int32)(arguments[5])
	userdata := *(*uintptr)(arguments[6])

	action := int32(Disarm)
	if ctx, ok := udpReadCallbackRegistry.Load(userdata); ok {
		readCtx := ctx.(udpReadContext)
		var buf []byte
		if bytesRead > 0 {
			buf = readCtx.buf[:bytesRead]
		}
		action = int32(readCtx.cb(
			(*Loop)(loop),
			(*UDPCompletion)(completion),
			(*Sockaddr)(remoteAddr),
			buf,
			bytesRead,
			errCode,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

func udpWriteTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 5)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	bytesWritten := *(*int32)(arguments[2])
	errCode := *(*int32)(arguments[3])
	userdata := *(*uintptr)(arguments[4])

	action := int32(Disarm)
	if cb, ok := udpWriteCallbackRegistry.Load(userdata); ok {
		action = int32(cb.(UDPWriteCallback)(
			(*Loop)(loop),
			(*UDPCompletion)(completion),
			bytesWritten,
			errCode,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

func udpTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 4)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	result := *(*int32)(arguments[2])
	userdata := *(*uintptr)(arguments[3])

	action := int32(Disarm)
	if cb, ok := udpCallbackRegistry.Load(userdata); ok {
		action = int32(cb.(UDPCallback)(
			(*Loop)(loop),
			(*UDPCompletion)(completion),
			result,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

// RegisterUDPReadCallback registers a UDP read callback with its buffer.
func RegisterUDPReadCallback(cb UDPReadCallback, buf []byte) uintptr {
	id := uintptr(atomic.AddUint64(&udpCallbackCounter, 1))
	udpReadCallbackRegistry.Store(id, udpReadContext{cb: cb, buf: buf})
	return id
}

// RegisterUDPWriteCallback registers a UDP write callback.
func RegisterUDPWriteCallback(cb UDPWriteCallback) uintptr {
	id := uintptr(atomic.AddUint64(&udpCallbackCounter, 1))
	udpWriteCallbackRegistry.Store(id, cb)
	return id
}

// RegisterUDPCallback registers a UDP callback.
func RegisterUDPCallback(cb UDPCallback) uintptr {
	id := uintptr(atomic.AddUint64(&udpCallbackCounter, 1))
	udpCallbackRegistry.Store(id, cb)
	return id
}

// UnregisterUDPCallback removes a UDP callback from all registries.
func UnregisterUDPCallback(id uintptr) {
	udpReadCallbackRegistry.Delete(id)
	udpWriteCallbackRegistry.Delete(id)
	udpCallbackRegistry.Delete(id)
}

// GetUDPReadCallbackPtr returns the C function pointer for read callbacks.
func GetUDPReadCallbackPtr() uintptr {
	initUDPClosures()
	return udpReadCallbackPtr
}

// GetUDPWriteCallbackPtr returns the C function pointer for write callbacks.
func GetUDPWriteCallbackPtr() uintptr {
	initUDPClosures()
	return udpWriteCallbackPtr
}

// GetUDPCallbackPtr returns the C function pointer for simple callbacks.
func GetUDPCallbackPtr() uintptr {
	initUDPClosures()
	return udpCallbackPtr
}

// UDPRead starts reading from a UDP socket.
func UDPRead(udp *UDP, loop *Loop, c *UDPCompletion, state *UDPState, buf []byte, userdata, cb uintptr) {
	udpPtr := unsafe.Pointer(udp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	statePtr := unsafe.Pointer(state)
	bufPtr := unsafe.Pointer(&buf[0])
	bufLen := uint64(len(buf))
	fnUDPRead.Call(nil, &udpPtr, &loopPtr, &cPtr, &statePtr, &bufPtr, &bufLen, &userdata, &cb)
}

// UDPReadWithCallback is a convenience function that registers the callback and starts reading.
func UDPReadWithCallback(udp *UDP, loop *Loop, c *UDPCompletion, state *UDPState, buf []byte, cb UDPReadCallback) uintptr {
	initUDPClosures()
	id := RegisterUDPReadCallback(cb, buf)
	UDPRead(udp, loop, c, state, buf, id, udpReadCallbackPtr)
	return id
}

// UDPWrite starts writing to a UDP socket.
func UDPWrite(udp *UDP, loop *Loop, c *UDPCompletion, state *UDPState, addr *Sockaddr, buf []byte, userdata, cb uintptr) {
	udpPtr := unsafe.Pointer(udp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	statePtr := unsafe.Pointer(state)
	addrPtr := unsafe.Pointer(addr)
	bufPtr := unsafe.Pointer(&buf[0])
	bufLen := uint64(len(buf))
	fnUDPWrite.Call(nil, &udpPtr, &loopPtr, &cPtr, &statePtr, &addrPtr, &bufPtr, &bufLen, &userdata, &cb)
}

// UDPWriteWithCallback is a convenience function that registers the callback and starts writing.
func UDPWriteWithCallback(udp *UDP, loop *Loop, c *UDPCompletion, state *UDPState, addr *Sockaddr, buf []byte, cb UDPWriteCallback) uintptr {
	initUDPClosures()
	id := RegisterUDPWriteCallback(cb)
	UDPWrite(udp, loop, c, state, addr, buf, id, udpWriteCallbackPtr)
	return id
}

// UDPClose starts closing a UDP socket.
func UDPClose(udp *UDP, loop *Loop, c *UDPCompletion, userdata, cb uintptr) {
	udpPtr := unsafe.Pointer(udp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	fnUDPClose.Call(nil, &udpPtr, &loopPtr, &cPtr, &userdata, &cb)
}

// UDPCloseWithCallback is a convenience function that registers the callback and starts closing.
func UDPCloseWithCallback(udp *UDP, loop *Loop, c *UDPCompletion, cb UDPCallback) uintptr {
	initUDPClosures()
	id := RegisterUDPCallback(cb)
	UDPClose(udp, loop, c, id, udpCallbackPtr)
	return id
}
