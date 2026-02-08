/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"errors"
	"net"

	"github.com/crrow/libxev-go/pkg/cxev"
)

// ErrExtLibNotLoaded is returned when TCP/UDP/File operations are attempted
// but the extended library is not available. Set LIBXEV_EXT_PATH environment
// variable to the path of libxev_extended.dylib/.so/.dll.
var ErrExtLibNotLoaded = errors.New("extended library (TCP support) not loaded; set LIBXEV_EXT_PATH")

// ErrEmptyBuffer is returned when an async read/write API is called with an empty buffer.
var ErrEmptyBuffer = errors.New("buffer cannot be empty")

// TCPListener accepts incoming TCP connections.
//
// Create a listener with [Listen], then call [TCPListener.Accept] or
// [TCPListener.AcceptFunc] to start accepting connections. Each accepted
// connection is delivered to the handler as a [TCPConn].
//
// # Example
//
//	listener, err := xev.Listen("tcp", "127.0.0.1:8080")
//	if err != nil {
//	    return err
//	}
//	defer listener.Close()
//
//	listener.AcceptFunc(loop, func(l *xev.TCPListener, conn *xev.TCPConn, err error) xev.Action {
//	    if err != nil {
//	        log.Printf("Accept error: %v", err)
//	        return xev.Stop
//	    }
//	    go handleConnection(conn)
//	    return xev.Continue // Keep accepting
//	})
type TCPListener struct {
	tcp        cxev.TCP
	completion cxev.TCPCompletion
	addr       cxev.Sockaddr
	callbackID uintptr
	loop       *Loop
	handler    AcceptHandler
}

// TCPConn represents an established TCP connection.
//
// TCPConn provides async read, write, and close operations. All operations
// are non-blocking and completion is signaled through callbacks.
//
// # Reading Data
//
// Use [TCPConn.Read] or [TCPConn.ReadFunc] to read data. The callback receives
// the data read and any error. Return [Continue] to keep reading, or [Stop]
// when done:
//
//	buf := make([]byte, 4096)
//	conn.ReadFunc(loop, buf, func(c *xev.TCPConn, data []byte, err error) xev.Action {
//	    if err != nil || len(data) == 0 {
//	        return xev.Stop // Connection closed or error
//	    }
//	    process(data)
//	    return xev.Continue // Keep reading
//	})
//
// # Writing Data
//
// Use [TCPConn.Write] or [TCPConn.WriteFunc] to write data:
//
//	conn.WriteFunc(loop, []byte("Hello"), func(c *xev.TCPConn, n int, err error) xev.Action {
//	    if err != nil {
//	        log.Printf("Write error: %v", err)
//	    }
//	    return xev.Stop
//	})
//
// # Closing
//
// Always close connections when done using [TCPConn.Close] or [TCPConn.CloseFunc].
type TCPConn struct {
	tcp          cxev.TCP
	completion   cxev.TCPCompletion
	fd           int32
	readBuf      []byte
	callbackID   uintptr
	loop         *Loop
	readHandler  ReadHandler
	writeHandler WriteHandler
	closeHandler CloseHandler
}

// AcceptHandler handles accepted TCP connections.
//
// Implement this interface for stateful accept handling. For simple use cases,
// [AcceptFunc] provides a more convenient functional approach.
type AcceptHandler interface {
	// OnAccept is called when a new connection is accepted.
	// conn is nil if err is non-nil.
	// Return [Continue] to keep accepting connections, or [Stop] to stop.
	OnAccept(listener *TCPListener, conn *TCPConn, err error) Action
}

// AcceptFunc is a function adapter for [AcceptHandler].
type AcceptFunc func(listener *TCPListener, conn *TCPConn, err error) Action

// OnAccept implements [AcceptHandler].
func (f AcceptFunc) OnAccept(l *TCPListener, c *TCPConn, err error) Action {
	return f(l, c, err)
}

// ReadHandler handles TCP read completions.
//
// Implement this interface for stateful read handling. For simple use cases,
// [ReadFunc] provides a more convenient functional approach.
type ReadHandler interface {
	// OnRead is called when data is read or an error occurs.
	// data may be empty on EOF or error.
	// Return [Continue] to keep reading, or [Stop] to stop.
	OnRead(conn *TCPConn, data []byte, err error) Action
}

// ReadFunc is a function adapter for [ReadHandler].
type ReadFunc func(conn *TCPConn, data []byte, err error) Action

// OnRead implements [ReadHandler].
func (f ReadFunc) OnRead(c *TCPConn, data []byte, err error) Action {
	return f(c, data, err)
}

// WriteHandler handles TCP write completions.
//
// Implement this interface for stateful write handling. For simple use cases,
// [WriteFunc] provides a more convenient functional approach.
type WriteHandler interface {
	// OnWrite is called when a write completes.
	// bytesWritten is the number of bytes successfully written.
	// Return [Continue] for chained writes, or [Stop] when done.
	OnWrite(conn *TCPConn, bytesWritten int, err error) Action
}

// WriteFunc is a function adapter for [WriteHandler].
type WriteFunc func(conn *TCPConn, bytesWritten int, err error) Action

// OnWrite implements [WriteHandler].
func (f WriteFunc) OnWrite(c *TCPConn, bytesWritten int, err error) Action {
	return f(c, bytesWritten, err)
}

// CloseHandler handles TCP close completions.
//
// Implement this interface if you need notification when a close completes.
// For simple use cases, [CloseFunc] provides a more convenient approach.
type CloseHandler interface {
	// OnClose is called when the connection is fully closed.
	OnClose(conn *TCPConn, err error)
}

// CloseFunc is a function adapter for [CloseHandler].
type CloseFunc func(conn *TCPConn, err error)

// OnClose implements [CloseHandler].
func (f CloseFunc) OnClose(c *TCPConn, err error) {
	if f != nil {
		f(c, err)
	}
}

// Listen creates a TCP listener bound to the specified address.
//
// The network parameter must be "tcp" (IPv4 support only currently).
// The address should be in "host:port" format, e.g., "127.0.0.1:8080" or
// "0.0.0.0:8080" for all interfaces.
//
// Returns [ErrExtLibNotLoaded] if the extended library is not available.
//
// Example:
//
//	listener, err := xev.Listen("tcp", "0.0.0.0:8080")
//	if err != nil {
//	    return err
//	}
//	defer listener.Close()
func Listen(network, address string) (*TCPListener, error) {
	if !cxev.ExtLibLoaded() {
		return nil, ErrExtLibNotLoaded
	}

	host, port, err := parseAddress(address)
	if err != nil {
		return nil, err
	}

	listener := &TCPListener{}

	if err := cxev.TCPInit(&listener.tcp, cxev.AF_INET()); err != nil {
		return nil, err
	}

	cxev.SockaddrIPv4(&listener.addr, host[0], host[1], host[2], host[3], port)

	if err := cxev.TCPBind(&listener.tcp, &listener.addr); err != nil {
		return nil, err
	}

	if err := cxev.TCPListen(&listener.tcp, 128); err != nil {
		return nil, err
	}

	return listener, nil
}

// Accept starts accepting connections using a handler interface.
//
// The handler's OnAccept method is called for each accepted connection.
// Return [Continue] from OnAccept to keep accepting, or [Stop] to stop.
func (l *TCPListener) Accept(loop *Loop, handler AcceptHandler) error {
	l.loop = loop
	l.handler = handler

	l.callbackID = cxev.TCPAcceptWithCallback(&l.tcp, &loop.inner, &l.completion, l.acceptCallback)
	return nil
}

// AcceptFunc starts accepting connections using a callback function.
//
// This is a convenience wrapper around [TCPListener.Accept] for functional-style
// callbacks.
func (l *TCPListener) AcceptFunc(loop *Loop, fn func(listener *TCPListener, conn *TCPConn, err error) Action) error {
	return l.Accept(loop, AcceptFunc(fn))
}

func (l *TCPListener) acceptCallback(loop *cxev.Loop, c *cxev.TCPCompletion, fd int32, errCode int32, userdata uintptr) cxev.CbAction {
	var err error
	var conn *TCPConn

	if errCode != 0 {
		err = errors.New("accept error")
	} else {
		conn = &TCPConn{fd: fd}
		cxev.TCPInitFd(&conn.tcp, fd)
	}

	action := l.handler.OnAccept(l, conn, err)
	if action == Continue {
		return cxev.Rearm
	}
	return cxev.Disarm
}

// Addr returns the local address the listener is bound to.
// Returns the host (always "0.0.0.0" currently) and port number.
func (l *TCPListener) Addr() (string, uint16) {
	var addr cxev.Sockaddr
	cxev.TCPGetsockname(&l.tcp, &addr)
	port := cxev.SockaddrPort(&addr)
	return "0.0.0.0", port
}

// Close stops accepting connections and releases listener resources.
//
// This should be called when the listener is no longer needed.
func (l *TCPListener) Close() {
	if l.callbackID != 0 {
		cxev.UnregisterTCPCallback(l.callbackID)
		l.callbackID = 0
	}
}

// Dial creates a TCP connection ready to connect to an address.
//
// This creates the socket but does not connect yet. Call [TCPConn.Connect]
// to initiate the async connection.
//
// Returns [ErrExtLibNotLoaded] if the extended library is not available.
func Dial(network, address string) (*TCPConn, error) {
	if !cxev.ExtLibLoaded() {
		return nil, ErrExtLibNotLoaded
	}

	host, port, err := parseAddress(address)
	if err != nil {
		return nil, err
	}

	conn := &TCPConn{}

	if err := cxev.TCPInit(&conn.tcp, cxev.AF_INET()); err != nil {
		return nil, err
	}

	var addr cxev.Sockaddr
	cxev.SockaddrIPv4(&addr, host[0], host[1], host[2], host[3], port)

	return conn, nil
}

// Connect initiates an async connection to the specified address.
//
// The handler is called when the connection completes (success or failure).
// On success, err is nil and the connection is ready for read/write operations.
//
// Example:
//
//	conn, _ := xev.Dial("tcp", "")
//	conn.Connect(loop, "127.0.0.1:8080", func(c *xev.TCPConn, err error) xev.Action {
//	    if err != nil {
//	        log.Printf("Connect failed: %v", err)
//	        return xev.Stop
//	    }
//	    // Connection established, start reading/writing
//	    return xev.Stop
//	})
func (c *TCPConn) Connect(loop *Loop, address string, handler func(conn *TCPConn, err error) Action) error {
	c.loop = loop

	host, port, err := parseAddress(address)
	if err != nil {
		return err
	}

	var addr cxev.Sockaddr
	cxev.SockaddrIPv4(&addr, host[0], host[1], host[2], host[3], port)

	c.callbackID = cxev.TCPConnectWithCallback(&c.tcp, &loop.inner, &c.completion, &addr, func(loop *cxev.Loop, comp *cxev.TCPCompletion, result int32, userdata uintptr) cxev.CbAction {
		var err error
		if result != 0 {
			err = errors.New("connect error")
		}
		action := handler(c, err)
		if action == Continue {
			return cxev.Rearm
		}
		return cxev.Disarm
	})

	return nil
}

// Read starts an async read operation using a handler interface.
//
// The handler's OnRead method is called when data is available or an error
// occurs. Return [Continue] to keep reading, or [Stop] to stop.
//
// The provided buffer is used for the read operation. The data slice passed
// to the handler is a slice of this buffer containing the bytes read.
func (c *TCPConn) Read(loop *Loop, buf []byte, handler ReadHandler) error {
	if len(buf) == 0 {
		return ErrEmptyBuffer
	}

	c.loop = loop
	c.readHandler = handler
	c.readBuf = buf

	c.callbackID = cxev.TCPReadWithCallback(&c.tcp, &loop.inner, &c.completion, buf, c.readCallback)
	return nil
}

// ReadFunc starts an async read operation using a callback function.
//
// This is a convenience wrapper around [TCPConn.Read] for functional-style callbacks.
func (c *TCPConn) ReadFunc(loop *Loop, buf []byte, fn func(conn *TCPConn, data []byte, err error) Action) error {
	return c.Read(loop, buf, ReadFunc(fn))
}

func (c *TCPConn) readCallback(loop *cxev.Loop, comp *cxev.TCPCompletion, data []byte, bytesRead int32, errCode int32, userdata uintptr) cxev.CbAction {
	var err error
	if errCode != 0 {
		err = errors.New("read error")
	}

	action := c.readHandler.OnRead(c, data, err)
	if action == Continue {
		return cxev.Rearm
	}
	return cxev.Disarm
}

// Write starts an async write operation using a handler interface.
//
// The handler's OnWrite method is called when the write completes. The
// bytesWritten parameter indicates how many bytes were successfully written.
func (c *TCPConn) Write(loop *Loop, data []byte, handler WriteHandler) error {
	if len(data) == 0 {
		return ErrEmptyBuffer
	}

	c.loop = loop
	c.writeHandler = handler

	c.callbackID = cxev.TCPWriteWithCallback(&c.tcp, &loop.inner, &c.completion, data, c.writeCallback)
	return nil
}

// WriteFunc starts an async write operation using a callback function.
//
// This is a convenience wrapper around [TCPConn.Write] for functional-style callbacks.
func (c *TCPConn) WriteFunc(loop *Loop, data []byte, fn func(conn *TCPConn, bytesWritten int, err error) Action) error {
	return c.Write(loop, data, WriteFunc(fn))
}

func (c *TCPConn) writeCallback(loop *cxev.Loop, comp *cxev.TCPCompletion, bytesWritten int32, errCode int32, userdata uintptr) cxev.CbAction {
	var err error
	if errCode != 0 {
		err = errors.New("write error")
	}

	action := c.writeHandler.OnWrite(c, int(bytesWritten), err)
	if action == Continue {
		return cxev.Rearm
	}
	return cxev.Disarm
}

// Close starts an async close operation.
//
// The handler (if non-nil) is called when the close completes. After close
// completes, the connection must not be used.
func (c *TCPConn) Close(loop *Loop, handler CloseHandler) error {
	c.loop = loop
	c.closeHandler = handler

	cxev.TCPCloseWithCallback(&c.tcp, &loop.inner, &c.completion, func(loop *cxev.Loop, comp *cxev.TCPCompletion, result int32, userdata uintptr) cxev.CbAction {
		var err error
		if result != 0 {
			err = errors.New("close error")
		}
		if c.closeHandler != nil {
			c.closeHandler.OnClose(c, err)
		}
		return cxev.Disarm
	})
	return nil
}

// CloseFunc starts an async close operation using a callback function.
//
// This is a convenience wrapper around [TCPConn.Close] for functional-style callbacks.
func (c *TCPConn) CloseFunc(loop *Loop, fn func(conn *TCPConn, err error)) error {
	return c.Close(loop, CloseFunc(fn))
}

// Fd returns the underlying file descriptor for advanced use cases.
func (c *TCPConn) Fd() int32 {
	return c.fd
}

// parseAddress parses a "host:port" string into IP bytes and port number.
func parseAddress(address string) ([4]byte, uint16, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return [4]byte{}, 0, err
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return [4]byte{}, 0, errors.New("invalid IP address")
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return [4]byte{}, 0, errors.New("IPv6 not yet supported")
	}

	var port int
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}

	return [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]}, uint16(port), nil
}
