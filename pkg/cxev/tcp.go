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

// TCP-related sizes from the extended API.
const (
	SizeofTCP           = 16  // xev_tcp: socket storage
	SizeofSockaddr      = 128 // xev_sockaddr: address storage
	SizeofTCPCompletion = 320 // Extended completion with callback pointer
)

// TCP represents a TCP socket.
type TCP [SizeofTCP]byte

// Sockaddr represents a socket address (IPv4 or IPv6).
type Sockaddr [SizeofSockaddr]byte

// TCPCompletion is an extended completion for TCP operations.
// It includes extra space for the C callback pointer.
type TCPCompletion [SizeofTCPCompletion]byte

// Address family constants.
var (
	afInet  int32
	afInet6 int32
)

// FFI function descriptors for TCP operations.
var (
	fnTCPInit        ffi.Fun
	fnTCPInitFd      ffi.Fun
	fnTCPFd          ffi.Fun
	fnTCPBind        ffi.Fun
	fnTCPListen      ffi.Fun
	fnTCPGetsockname ffi.Fun
	fnTCPAccept      ffi.Fun
	fnTCPConnect     ffi.Fun
	fnTCPRead        ffi.Fun
	fnTCPWrite       ffi.Fun
	fnTCPClose       ffi.Fun
	fnTCPShutdown    ffi.Fun
	fnSockaddrIPv4   ffi.Fun
	fnSockaddrIPv6   ffi.Fun
	fnSockaddrPort   ffi.Fun
	fnAfInet         ffi.Fun
	fnAfInet6        ffi.Fun
)

func registerTCPFunctions() error {
	var err error

	// int xev_tcp_init(xev_tcp* tcp, int family)
	fnTCPInit, err = libExt.Prep("xev_tcp_init", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// void xev_tcp_init_fd(xev_tcp* tcp, int fd)
	fnTCPInitFd, err = libExt.Prep("xev_tcp_init_fd", &ffi.TypeVoid, &ffi.TypePointer, &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// int xev_tcp_fd(xev_tcp* tcp)
	fnTCPFd, err = libExt.Prep("xev_tcp_fd", &ffi.TypeSint32, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// int xev_tcp_bind(xev_tcp* tcp, xev_sockaddr* addr)
	fnTCPBind, err = libExt.Prep("xev_tcp_bind", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// int xev_tcp_listen(xev_tcp* tcp, int backlog)
	fnTCPListen, err = libExt.Prep("xev_tcp_listen", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// int xev_tcp_getsockname(xev_tcp* tcp, xev_sockaddr* addr)
	fnTCPGetsockname, err = libExt.Prep("xev_tcp_getsockname", &ffi.TypeSint32, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_tcp_accept(xev_tcp*, xev_loop*, xev_completion*, void* userdata, callback)
	fnTCPAccept, err = libExt.Prep("xev_tcp_accept", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_tcp_connect(xev_tcp*, xev_loop*, xev_completion*, xev_sockaddr*, void* userdata, callback)
	fnTCPConnect, err = libExt.Prep("xev_tcp_connect", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_tcp_read(xev_tcp*, xev_loop*, xev_completion*, buf, buf_len, void* userdata, callback)
	fnTCPRead, err = libExt.Prep("xev_tcp_read", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_tcp_write(xev_tcp*, xev_loop*, xev_completion*, buf, buf_len, void* userdata, callback)
	fnTCPWrite, err = libExt.Prep("xev_tcp_write", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeUint64, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_tcp_close(xev_tcp*, xev_loop*, xev_completion*, void* userdata, callback)
	fnTCPClose, err = libExt.Prep("xev_tcp_close", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_tcp_shutdown(xev_tcp*, xev_loop*, xev_completion*, void* userdata, callback)
	fnTCPShutdown, err = libExt.Prep("xev_tcp_shutdown", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// void xev_sockaddr_ipv4(xev_sockaddr*, u8, u8, u8, u8, u16)
	fnSockaddrIPv4, err = libExt.Prep("xev_sockaddr_ipv4", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypeUint8, &ffi.TypeUint8, &ffi.TypeUint8, &ffi.TypeUint8, &ffi.TypeUint16)
	if err != nil {
		return err
	}

	// void xev_sockaddr_ipv6(xev_sockaddr*, u8[16]*, u16, u32, u32)
	fnSockaddrIPv6, err = libExt.Prep("xev_sockaddr_ipv6", &ffi.TypeVoid,
		&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeUint16, &ffi.TypeUint32, &ffi.TypeUint32)
	if err != nil {
		return err
	}

	// u16 xev_sockaddr_port(xev_sockaddr*)
	fnSockaddrPort, err = libExt.Prep("xev_sockaddr_port", &ffi.TypeUint16, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// int xev_af_inet()
	fnAfInet, err = libExt.Prep("xev_af_inet", &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// int xev_af_inet6()
	fnAfInet6, err = libExt.Prep("xev_af_inet6", &ffi.TypeSint32)
	if err != nil {
		return err
	}

	// Query AF_INET and AF_INET6 values from the library
	var ret ffi.Arg
	fnAfInet.Call(&ret)
	afInet = int32(ret)
	fnAfInet6.Call(&ret)
	afInet6 = int32(ret)

	return nil
}

// AF_INET returns the IPv4 address family constant.
func AF_INET() int32 { return afInet }

// AF_INET6 returns the IPv6 address family constant.
func AF_INET6() int32 { return afInet6 }

// TCPInit initializes a TCP socket with the given address family.
func TCPInit(tcp *TCP, family int32) error {
	if loadErr != nil {
		return loadErr
	}
	var ret ffi.Arg
	ptr := unsafe.Pointer(tcp)
	fnTCPInit.Call(&ret, &ptr, &family)
	if int32(ret) != 0 {
		return TCPError(int32(ret))
	}
	return nil
}

// TCPInitFd initializes a TCP socket from an existing file descriptor.
func TCPInitFd(tcp *TCP, fd int32) {
	ptr := unsafe.Pointer(tcp)
	fnTCPInitFd.Call(nil, &ptr, &fd)
}

// TCPFd returns the file descriptor of a TCP socket.
func TCPFd(tcp *TCP) int32 {
	var ret ffi.Arg
	ptr := unsafe.Pointer(tcp)
	fnTCPFd.Call(&ret, &ptr)
	return int32(ret)
}

// TCPBind binds a TCP socket to an address.
func TCPBind(tcp *TCP, addr *Sockaddr) error {
	var ret ffi.Arg
	tcpPtr := unsafe.Pointer(tcp)
	addrPtr := unsafe.Pointer(addr)
	fnTCPBind.Call(&ret, &tcpPtr, &addrPtr)
	if int32(ret) != 0 {
		return TCPError(int32(ret))
	}
	return nil
}

// TCPListen starts listening on a TCP socket.
func TCPListen(tcp *TCP, backlog int32) error {
	var ret ffi.Arg
	ptr := unsafe.Pointer(tcp)
	fnTCPListen.Call(&ret, &ptr, &backlog)
	if int32(ret) != 0 {
		return TCPError(int32(ret))
	}
	return nil
}

// TCPGetsockname gets the local address of a bound TCP socket.
func TCPGetsockname(tcp *TCP, addr *Sockaddr) error {
	var ret ffi.Arg
	tcpPtr := unsafe.Pointer(tcp)
	addrPtr := unsafe.Pointer(addr)
	fnTCPGetsockname.Call(&ret, &tcpPtr, &addrPtr)
	if int32(ret) != 0 {
		return TCPError(int32(ret))
	}
	return nil
}

// SockaddrIPv4 initializes a sockaddr for IPv4.
func SockaddrIPv4(addr *Sockaddr, a, b, c, d uint8, port uint16) {
	ptr := unsafe.Pointer(addr)
	fnSockaddrIPv4.Call(nil, &ptr, &a, &b, &c, &d, &port)
}

// SockaddrIPv6 initializes a sockaddr for IPv6.
func SockaddrIPv6(addr *Sockaddr, ip *[16]byte, port uint16, flowinfo, scopeID uint32) {
	addrPtr := unsafe.Pointer(addr)
	ipPtr := unsafe.Pointer(ip)
	fnSockaddrIPv6.Call(nil, &addrPtr, &ipPtr, &port, &flowinfo, &scopeID)
}

// SockaddrPort returns the port from a sockaddr.
func SockaddrPort(addr *Sockaddr) uint16 {
	var ret uint16
	ptr := unsafe.Pointer(addr)
	fnSockaddrPort.Call(&ret, &ptr)
	return ret
}

// TCPError represents an error from TCP operations.
type TCPError int32

func (e TCPError) Error() string {
	return "tcp error: " + string(rune(e))
}

// TCP Callback types

// TCPCallback is called for simple TCP operations (connect, close, shutdown).
type TCPCallback func(loop *Loop, c *TCPCompletion, result int32, userdata uintptr) CbAction

// TCPAcceptCallback is called when a connection is accepted.
type TCPAcceptCallback func(loop *Loop, c *TCPCompletion, acceptedFd int32, err int32, userdata uintptr) CbAction

// TCPReadCallback is called when data is read.
type TCPReadCallback func(loop *Loop, c *TCPCompletion, buf []byte, bytesRead int32, err int32, userdata uintptr) CbAction

// TCPWriteCallback is called when data is written.
type TCPWriteCallback func(loop *Loop, c *TCPCompletion, bytesWritten int32, err int32, userdata uintptr) CbAction

// TCP callback registry
var (
	tcpCallbackRegistry       sync.Map
	tcpAcceptCallbackRegistry sync.Map
	tcpReadCallbackRegistry   sync.Map
	tcpWriteCallbackRegistry  sync.Map
	tcpCallbackCounter        uint64
)

// TCP callback closure state
var (
	tcpClosureInit sync.Once

	tcpCallbackPtr       uintptr
	tcpAcceptCallbackPtr uintptr
	tcpReadCallbackPtr   uintptr
	tcpWriteCallbackPtr  uintptr

	tcpClosure       *ffi.Closure
	tcpClosureCode   unsafe.Pointer
	tcpAcceptClosure *ffi.Closure
	tcpAcceptCode    unsafe.Pointer
	tcpReadClosure   *ffi.Closure
	tcpReadCode      unsafe.Pointer
	tcpWriteClosure  *ffi.Closure
	tcpWriteCode     unsafe.Pointer

	tcpCif       ffi.Cif
	tcpAcceptCif ffi.Cif
	tcpReadCif   ffi.Cif
	tcpWriteCif  ffi.Cif
)

func initTCPClosures() {
	tcpClosureInit.Do(func() {
		// TCP simple callback: (loop*, completion*, result int32, userdata*) -> int32
		tcpClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &tcpClosureCode)
		if status := ffi.PrepCif(&tcpCif, ffi.DefaultAbi, 4,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare TCP callback CIF")
		}
		goCallback := ffi.NewCallback(tcpTrampoline)
		if status := ffi.PrepClosureLoc(tcpClosure, &tcpCif, goCallback, nil, tcpClosureCode); status != ffi.OK {
			panic("failed to prepare TCP closure")
		}
		tcpCallbackPtr = uintptr(tcpClosureCode)

		// TCP accept callback: (loop*, completion*, fd int32, err int32, userdata*) -> int32
		tcpAcceptClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &tcpAcceptCode)
		if status := ffi.PrepCif(&tcpAcceptCif, ffi.DefaultAbi, 5,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare TCP accept callback CIF")
		}
		goAcceptCallback := ffi.NewCallback(tcpAcceptTrampoline)
		if status := ffi.PrepClosureLoc(tcpAcceptClosure, &tcpAcceptCif, goAcceptCallback, nil, tcpAcceptCode); status != ffi.OK {
			panic("failed to prepare TCP accept closure")
		}
		tcpAcceptCallbackPtr = uintptr(tcpAcceptCode)

		// TCP read callback: (loop*, completion*, buf*, bytes_read int32, err int32, userdata*) -> int32
		tcpReadClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &tcpReadCode)
		if status := ffi.PrepCif(&tcpReadCif, ffi.DefaultAbi, 6,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare TCP read callback CIF")
		}
		goReadCallback := ffi.NewCallback(tcpReadTrampoline)
		if status := ffi.PrepClosureLoc(tcpReadClosure, &tcpReadCif, goReadCallback, nil, tcpReadCode); status != ffi.OK {
			panic("failed to prepare TCP read closure")
		}
		tcpReadCallbackPtr = uintptr(tcpReadCode)

		// TCP write callback: (loop*, completion*, bytes_written int32, err int32, userdata*) -> int32
		tcpWriteClosure = ffi.ClosureAlloc(unsafe.Sizeof(ffi.Closure{}), &tcpWriteCode)
		if status := ffi.PrepCif(&tcpWriteCif, ffi.DefaultAbi, 5,
			&ffi.TypeSint32,
			&ffi.TypePointer, &ffi.TypePointer, &ffi.TypeSint32, &ffi.TypeSint32, &ffi.TypePointer,
		); status != ffi.OK {
			panic("failed to prepare TCP write callback CIF")
		}
		goWriteCallback := ffi.NewCallback(tcpWriteTrampoline)
		if status := ffi.PrepClosureLoc(tcpWriteClosure, &tcpWriteCif, goWriteCallback, nil, tcpWriteCode); status != ffi.OK {
			panic("failed to prepare TCP write closure")
		}
		tcpWriteCallbackPtr = uintptr(tcpWriteCode)
	})
}

func tcpTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 4)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	result := *(*int32)(arguments[2])
	userdata := *(*uintptr)(arguments[3])

	action := int32(Disarm)
	if cb, ok := tcpCallbackRegistry.Load(userdata); ok {
		action = int32(cb.(TCPCallback)(
			(*Loop)(loop),
			(*TCPCompletion)(completion),
			result,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

func tcpAcceptTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 5)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	fd := *(*int32)(arguments[2])
	errCode := *(*int32)(arguments[3])
	userdata := *(*uintptr)(arguments[4])

	action := int32(Disarm)
	if cb, ok := tcpAcceptCallbackRegistry.Load(userdata); ok {
		action = int32(cb.(TCPAcceptCallback)(
			(*Loop)(loop),
			(*TCPCompletion)(completion),
			fd,
			errCode,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

// tcpReadContext holds the buffer pointer and length for read callbacks.
type tcpReadContext struct {
	cb     TCPReadCallback
	buf    []byte
	bufPtr uintptr
}

func tcpReadTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 6)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	_ = *(*unsafe.Pointer)(arguments[2]) // buf ptr (we use our stored slice)
	bytesRead := *(*int32)(arguments[3])
	errCode := *(*int32)(arguments[4])
	userdata := *(*uintptr)(arguments[5])

	action := int32(Disarm)
	if ctx, ok := tcpReadCallbackRegistry.Load(userdata); ok {
		readCtx := ctx.(tcpReadContext)
		var buf []byte
		if bytesRead > 0 {
			buf = readCtx.buf[:bytesRead]
		}
		action = int32(readCtx.cb(
			(*Loop)(loop),
			(*TCPCompletion)(completion),
			buf,
			bytesRead,
			errCode,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

func tcpWriteTrampoline(cif *ffi.Cif, ret unsafe.Pointer, args *unsafe.Pointer, userData unsafe.Pointer) uintptr {
	arguments := unsafe.Slice(args, 5)
	loop := *(*unsafe.Pointer)(arguments[0])
	completion := *(*unsafe.Pointer)(arguments[1])
	bytesWritten := *(*int32)(arguments[2])
	errCode := *(*int32)(arguments[3])
	userdata := *(*uintptr)(arguments[4])

	action := int32(Disarm)
	if cb, ok := tcpWriteCallbackRegistry.Load(userdata); ok {
		action = int32(cb.(TCPWriteCallback)(
			(*Loop)(loop),
			(*TCPCompletion)(completion),
			bytesWritten,
			errCode,
			userdata,
		))
	}
	*(*int32)(ret) = action
	return 0
}

// RegisterTCPCallback registers a TCP callback and returns its unique ID.
func RegisterTCPCallback(cb TCPCallback) uintptr {
	id := uintptr(atomic.AddUint64(&tcpCallbackCounter, 1))
	tcpCallbackRegistry.Store(id, cb)
	return id
}

// RegisterTCPAcceptCallback registers a TCP accept callback.
func RegisterTCPAcceptCallback(cb TCPAcceptCallback) uintptr {
	id := uintptr(atomic.AddUint64(&tcpCallbackCounter, 1))
	tcpAcceptCallbackRegistry.Store(id, cb)
	return id
}

// RegisterTCPReadCallback registers a TCP read callback with its buffer.
func RegisterTCPReadCallback(cb TCPReadCallback, buf []byte) uintptr {
	id := uintptr(atomic.AddUint64(&tcpCallbackCounter, 1))
	tcpReadCallbackRegistry.Store(id, tcpReadContext{cb: cb, buf: buf, bufPtr: uintptr(unsafe.Pointer(&buf[0]))})
	return id
}

// RegisterTCPWriteCallback registers a TCP write callback.
func RegisterTCPWriteCallback(cb TCPWriteCallback) uintptr {
	id := uintptr(atomic.AddUint64(&tcpCallbackCounter, 1))
	tcpWriteCallbackRegistry.Store(id, cb)
	return id
}

// UnregisterTCPCallback removes a TCP callback from the registry.
func UnregisterTCPCallback(id uintptr) {
	tcpCallbackRegistry.Delete(id)
	tcpAcceptCallbackRegistry.Delete(id)
	tcpReadCallbackRegistry.Delete(id)
	tcpWriteCallbackRegistry.Delete(id)
}

// GetTCPCallbackPtr returns the C function pointer for TCP callbacks.
func GetTCPCallbackPtr() uintptr {
	initTCPClosures()
	return tcpCallbackPtr
}

// GetTCPAcceptCallbackPtr returns the C function pointer for accept callbacks.
func GetTCPAcceptCallbackPtr() uintptr {
	initTCPClosures()
	return tcpAcceptCallbackPtr
}

// GetTCPReadCallbackPtr returns the C function pointer for read callbacks.
func GetTCPReadCallbackPtr() uintptr {
	initTCPClosures()
	return tcpReadCallbackPtr
}

// GetTCPWriteCallbackPtr returns the C function pointer for write callbacks.
func GetTCPWriteCallbackPtr() uintptr {
	initTCPClosures()
	return tcpWriteCallbackPtr
}

// TCPAccept starts accepting a connection on a listening socket.
func TCPAccept(tcp *TCP, loop *Loop, c *TCPCompletion, userdata, cb uintptr) {
	tcpPtr := unsafe.Pointer(tcp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	fnTCPAccept.Call(nil, &tcpPtr, &loopPtr, &cPtr, &userdata, &cb)
}

// TCPAcceptWithCallback is a convenience function that registers the callback and starts accepting.
func TCPAcceptWithCallback(tcp *TCP, loop *Loop, c *TCPCompletion, cb TCPAcceptCallback) uintptr {
	initTCPClosures()
	id := RegisterTCPAcceptCallback(cb)
	TCPAccept(tcp, loop, c, id, tcpAcceptCallbackPtr)
	return id
}

// TCPConnect starts connecting to a remote address.
func TCPConnect(tcp *TCP, loop *Loop, c *TCPCompletion, addr *Sockaddr, userdata, cb uintptr) {
	tcpPtr := unsafe.Pointer(tcp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	addrPtr := unsafe.Pointer(addr)
	fnTCPConnect.Call(nil, &tcpPtr, &loopPtr, &cPtr, &addrPtr, &userdata, &cb)
}

// TCPConnectWithCallback is a convenience function that registers the callback and starts connecting.
func TCPConnectWithCallback(tcp *TCP, loop *Loop, c *TCPCompletion, addr *Sockaddr, cb TCPCallback) uintptr {
	initTCPClosures()
	id := RegisterTCPCallback(cb)
	TCPConnect(tcp, loop, c, addr, id, tcpCallbackPtr)
	return id
}

// TCPRead starts reading from a TCP socket.
func TCPRead(tcp *TCP, loop *Loop, c *TCPCompletion, buf []byte, userdata, cb uintptr) {
	tcpPtr := unsafe.Pointer(tcp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	bufPtr := unsafe.Pointer(&buf[0])
	bufLen := uint64(len(buf))
	fnTCPRead.Call(nil, &tcpPtr, &loopPtr, &cPtr, &bufPtr, &bufLen, &userdata, &cb)
}

// TCPReadWithCallback is a convenience function that registers the callback and starts reading.
func TCPReadWithCallback(tcp *TCP, loop *Loop, c *TCPCompletion, buf []byte, cb TCPReadCallback) uintptr {
	initTCPClosures()
	id := RegisterTCPReadCallback(cb, buf)
	TCPRead(tcp, loop, c, buf, id, tcpReadCallbackPtr)
	return id
}

// TCPWrite starts writing to a TCP socket.
func TCPWrite(tcp *TCP, loop *Loop, c *TCPCompletion, buf []byte, userdata, cb uintptr) {
	tcpPtr := unsafe.Pointer(tcp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	bufPtr := unsafe.Pointer(&buf[0])
	bufLen := uint64(len(buf))
	fnTCPWrite.Call(nil, &tcpPtr, &loopPtr, &cPtr, &bufPtr, &bufLen, &userdata, &cb)
}

// TCPWriteWithCallback is a convenience function that registers the callback and starts writing.
func TCPWriteWithCallback(tcp *TCP, loop *Loop, c *TCPCompletion, buf []byte, cb TCPWriteCallback) uintptr {
	initTCPClosures()
	id := RegisterTCPWriteCallback(cb)
	TCPWrite(tcp, loop, c, buf, id, tcpWriteCallbackPtr)
	return id
}

// TCPClose starts closing a TCP socket.
func TCPClose(tcp *TCP, loop *Loop, c *TCPCompletion, userdata, cb uintptr) {
	tcpPtr := unsafe.Pointer(tcp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	fnTCPClose.Call(nil, &tcpPtr, &loopPtr, &cPtr, &userdata, &cb)
}

// TCPCloseWithCallback is a convenience function that registers the callback and starts closing.
func TCPCloseWithCallback(tcp *TCP, loop *Loop, c *TCPCompletion, cb TCPCallback) uintptr {
	initTCPClosures()
	id := RegisterTCPCallback(cb)
	TCPClose(tcp, loop, c, id, tcpCallbackPtr)
	return id
}

// TCPShutdown starts shutting down the write side of a TCP socket.
func TCPShutdown(tcp *TCP, loop *Loop, c *TCPCompletion, userdata, cb uintptr) {
	tcpPtr := unsafe.Pointer(tcp)
	loopPtr := unsafe.Pointer(loop)
	cPtr := unsafe.Pointer(c)
	fnTCPShutdown.Call(nil, &tcpPtr, &loopPtr, &cPtr, &userdata, &cb)
}

// TCPShutdownWithCallback is a convenience function.
func TCPShutdownWithCallback(tcp *TCP, loop *Loop, c *TCPCompletion, cb TCPCallback) uintptr {
	initTCPClosures()
	id := RegisterTCPCallback(cb)
	TCPShutdown(tcp, loop, c, id, tcpCallbackPtr)
	return id
}

// ExtLibLoaded returns true if the extended library (TCP support) is loaded.
func ExtLibLoaded() bool {
	return libExt.Addr != 0
}
