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

// UDPConn represents a UDP socket for sending and receiving datagrams.
//
// Unlike TCP, UDP is connectionless. Each datagram is independent and can
// be sent to or received from any address. UDPConn provides async operations
// for reading and writing datagrams.
//
// # Creating UDP Sockets
//
// Use [ListenUDP] to create a bound socket for receiving datagrams:
//
//	conn, err := xev.ListenUDP("udp", "0.0.0.0:8080")
//	if err != nil {
//	    return err
//	}
//	defer conn.Cleanup()
//
// Use [NewUDPConn] to create an unbound socket for sending only:
//
//	conn, err := xev.NewUDPConn()
//	if err != nil {
//	    return err
//	}
//	// Optionally bind later with conn.Bind("0.0.0.0:0")
//
// # Receiving Datagrams
//
// Use [UDPConn.ReadFrom] or [UDPConn.ReadFromFunc] to receive datagrams.
// The callback includes the remote address of the sender:
//
//	buf := make([]byte, 65535)
//	conn.ReadFromFunc(loop, buf, func(c *xev.UDPConn, data []byte, addr *net.UDPAddr, err error) xev.Action {
//	    if err != nil {
//	        return xev.Stop
//	    }
//	    fmt.Printf("Received %d bytes from %s\n", len(data), addr)
//	    return xev.Continue // Keep receiving
//	})
//
// # Sending Datagrams
//
// Use [UDPConn.WriteTo] or [UDPConn.WriteToAddr] to send datagrams:
//
//	// Send to address string
//	conn.WriteToFunc(loop, []byte("Hello"), "127.0.0.1:8080", func(c *xev.UDPConn, n int, err error) xev.Action {
//	    return xev.Stop
//	})
//
//	// Send to net.UDPAddr (useful for replying to received datagrams)
//	conn.WriteToAddrFunc(loop, []byte("Reply"), remoteAddr, func(c *xev.UDPConn, n int, err error) xev.Action {
//	    return xev.Stop
//	})
type UDPConn struct {
	udp        cxev.UDP
	completion cxev.UDPCompletion
	state      cxev.UDPState
	addr       cxev.Sockaddr
	readBuf    []byte
	callbackID uintptr
	loop       *Loop

	readHandler  UDPReadHandler
	writeHandler UDPWriteHandler
	closeHandler UDPCloseHandler
}

// UDPReadHandler handles received UDP datagrams.
//
// Implement this interface for stateful datagram handling. For simple use cases,
// [UDPReadFunc] provides a more convenient functional approach.
type UDPReadHandler interface {
	// OnRead is called when a datagram is received.
	// data contains the datagram payload.
	// remoteAddr is the sender's address (may be nil on error).
	// Return [Continue] to keep receiving, or [Stop] to stop.
	OnRead(conn *UDPConn, data []byte, remoteAddr *net.UDPAddr, err error) Action
}

// UDPReadFunc is a function adapter for [UDPReadHandler].
type UDPReadFunc func(conn *UDPConn, data []byte, remoteAddr *net.UDPAddr, err error) Action

// OnRead implements [UDPReadHandler].
func (f UDPReadFunc) OnRead(c *UDPConn, data []byte, addr *net.UDPAddr, err error) Action {
	return f(c, data, addr, err)
}

// UDPWriteHandler handles UDP write completions.
//
// Implement this interface for stateful write handling. For simple use cases,
// [UDPWriteFunc] provides a more convenient functional approach.
type UDPWriteHandler interface {
	// OnWrite is called when a write completes.
	// bytesWritten is the number of bytes sent.
	// Return [Continue] for chained writes, or [Stop] when done.
	OnWrite(conn *UDPConn, bytesWritten int, err error) Action
}

// UDPWriteFunc is a function adapter for [UDPWriteHandler].
type UDPWriteFunc func(conn *UDPConn, bytesWritten int, err error) Action

// OnWrite implements [UDPWriteHandler].
func (f UDPWriteFunc) OnWrite(c *UDPConn, bytesWritten int, err error) Action {
	return f(c, bytesWritten, err)
}

// UDPCloseHandler handles UDP close completions.
//
// Implement this interface if you need notification when a close completes.
// For simple use cases, [UDPCloseFunc] provides a more convenient approach.
type UDPCloseHandler interface {
	// OnClose is called when the socket is fully closed.
	OnClose(conn *UDPConn, err error)
}

// UDPCloseFunc is a function adapter for [UDPCloseHandler].
type UDPCloseFunc func(conn *UDPConn, err error)

// OnClose implements [UDPCloseHandler].
func (f UDPCloseFunc) OnClose(c *UDPConn, err error) {
	if f != nil {
		f(c, err)
	}
}

// ListenUDP creates a UDP socket bound to the specified address.
//
// The network parameter should be "udp" (IPv4 support only currently).
// The address should be in "host:port" format. Use "0.0.0.0:port" to listen
// on all interfaces, or "0.0.0.0:0" to let the OS assign a port.
//
// Returns [ErrExtLibNotLoaded] if the extended library is not available.
//
// Example:
//
//	// Listen on specific port
//	conn, err := xev.ListenUDP("udp", "0.0.0.0:8080")
//
//	// Listen on OS-assigned port
//	conn, err := xev.ListenUDP("udp", "0.0.0.0:0")
//	_, port := conn.LocalAddr()
//	fmt.Printf("Listening on port %d\n", port)
func ListenUDP(network, address string) (*UDPConn, error) {
	if !cxev.ExtLibLoaded() {
		return nil, ErrExtLibNotLoaded
	}

	host, port, err := parseAddress(address)
	if err != nil {
		return nil, err
	}

	conn := &UDPConn{}

	if err := cxev.UDPInit(&conn.udp, cxev.AF_INET()); err != nil {
		return nil, err
	}

	cxev.SockaddrIPv4(&conn.addr, host[0], host[1], host[2], host[3], port)

	if err := cxev.UDPBind(&conn.udp, &conn.addr); err != nil {
		return nil, err
	}

	return conn, nil
}

// NewUDPConn creates an unbound UDP socket.
//
// Use this when you need to send datagrams without receiving, or when you
// want to bind manually using [UDPConn.Bind].
//
// Returns [ErrExtLibNotLoaded] if the extended library is not available.
func NewUDPConn() (*UDPConn, error) {
	if !cxev.ExtLibLoaded() {
		return nil, ErrExtLibNotLoaded
	}

	conn := &UDPConn{}

	if err := cxev.UDPInit(&conn.udp, cxev.AF_INET()); err != nil {
		return nil, err
	}

	return conn, nil
}

// Bind binds the UDP socket to the specified address.
//
// This is typically used after [NewUDPConn] to bind the socket before
// receiving datagrams. Use "0.0.0.0:0" to let the OS assign an address.
func (c *UDPConn) Bind(address string) error {
	host, port, err := parseAddress(address)
	if err != nil {
		return err
	}

	cxev.SockaddrIPv4(&c.addr, host[0], host[1], host[2], host[3], port)
	return cxev.UDPBind(&c.udp, &c.addr)
}

// LocalAddr returns the local address the socket is bound to.
// Returns the host (always "0.0.0.0" currently) and port number.
func (c *UDPConn) LocalAddr() (string, uint16) {
	var addr cxev.Sockaddr
	cxev.UDPGetsockname(&c.udp, &addr)
	port := cxev.SockaddrPort(&addr)
	return "0.0.0.0", port
}

// ReadFrom starts an async receive operation using a handler interface.
//
// The handler's OnRead method is called when a datagram is received. The
// remoteAddr parameter contains the sender's address, which can be used
// to send a reply.
//
// Return [Continue] from the handler to keep receiving, or [Stop] to stop.
func (c *UDPConn) ReadFrom(loop *Loop, buf []byte, handler UDPReadHandler) error {
	if len(buf) == 0 {
		return ErrEmptyBuffer
	}

	c.loop = loop
	c.readHandler = handler
	c.readBuf = buf

	c.callbackID = cxev.UDPReadWithCallback(&c.udp, &loop.inner, &c.completion, &c.state, buf, c.readCallback)
	return nil
}

// ReadFromFunc starts an async receive operation using a callback function.
//
// This is a convenience wrapper around [UDPConn.ReadFrom] for functional-style
// callbacks.
func (c *UDPConn) ReadFromFunc(loop *Loop, buf []byte, fn func(conn *UDPConn, data []byte, remoteAddr *net.UDPAddr, err error) Action) error {
	return c.ReadFrom(loop, buf, UDPReadFunc(fn))
}

func (c *UDPConn) readCallback(loop *cxev.Loop, comp *cxev.UDPCompletion, remoteAddr *cxev.Sockaddr, data []byte, bytesRead int32, errCode int32, userdata uintptr) cxev.CbAction {
	var err error
	if errCode != 0 {
		err = errors.New("read error")
	}

	var addr *net.UDPAddr
	if remoteAddr != nil {
		addr = sockaddrToUDPAddr(remoteAddr)
	}

	action := c.readHandler.OnRead(c, data, addr, err)
	if action == Continue {
		return cxev.Rearm
	}
	return cxev.Disarm
}

// WriteTo starts an async send operation to the specified address string.
//
// The address should be in "host:port" format.
func (c *UDPConn) WriteTo(loop *Loop, data []byte, address string, handler UDPWriteHandler) error {
	if len(data) == 0 {
		return ErrEmptyBuffer
	}

	c.loop = loop
	c.writeHandler = handler

	host, port, err := parseAddress(address)
	if err != nil {
		return err
	}

	var addr cxev.Sockaddr
	cxev.SockaddrIPv4(&addr, host[0], host[1], host[2], host[3], port)

	c.callbackID = cxev.UDPWriteWithCallback(&c.udp, &loop.inner, &c.completion, &c.state, &addr, data, c.writeCallback)
	return nil
}

// WriteToFunc starts an async send using a callback function.
//
// This is a convenience wrapper around [UDPConn.WriteTo] for functional-style
// callbacks.
func (c *UDPConn) WriteToFunc(loop *Loop, data []byte, address string, fn func(conn *UDPConn, bytesWritten int, err error) Action) error {
	return c.WriteTo(loop, data, address, UDPWriteFunc(fn))
}

// WriteToAddr starts an async send operation to a [net.UDPAddr].
//
// This is useful for replying to received datagrams, where you already have
// the sender's address from the read callback.
func (c *UDPConn) WriteToAddr(loop *Loop, data []byte, addr *net.UDPAddr, handler UDPWriteHandler) error {
	if addr == nil {
		return errors.New("address is nil")
	}
	if len(data) == 0 {
		return ErrEmptyBuffer
	}

	c.loop = loop
	c.writeHandler = handler

	ip4 := addr.IP.To4()
	if ip4 == nil {
		return errors.New("IPv6 not yet supported")
	}

	var sockaddr cxev.Sockaddr
	cxev.SockaddrIPv4(&sockaddr, ip4[0], ip4[1], ip4[2], ip4[3], uint16(addr.Port))

	c.callbackID = cxev.UDPWriteWithCallback(&c.udp, &loop.inner, &c.completion, &c.state, &sockaddr, data, c.writeCallback)
	return nil
}

// WriteToAddrFunc starts an async send to a [net.UDPAddr] using a callback function.
//
// This is a convenience wrapper around [UDPConn.WriteToAddr] for functional-style
// callbacks.
func (c *UDPConn) WriteToAddrFunc(loop *Loop, data []byte, addr *net.UDPAddr, fn func(conn *UDPConn, bytesWritten int, err error) Action) error {
	return c.WriteToAddr(loop, data, addr, UDPWriteFunc(fn))
}

func (c *UDPConn) writeCallback(loop *cxev.Loop, comp *cxev.UDPCompletion, bytesWritten int32, errCode int32, userdata uintptr) cxev.CbAction {
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
// completes, call [UDPConn.Cleanup] to release callback resources.
func (c *UDPConn) Close(loop *Loop, handler UDPCloseHandler) error {
	c.loop = loop
	c.closeHandler = handler

	cxev.UDPCloseWithCallback(&c.udp, &loop.inner, &c.completion, func(loop *cxev.Loop, comp *cxev.UDPCompletion, result int32, userdata uintptr) cxev.CbAction {
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

// CloseFunc starts an async close using a callback function.
//
// This is a convenience wrapper around [UDPConn.Close] for functional-style
// callbacks.
func (c *UDPConn) CloseFunc(loop *Loop, fn func(conn *UDPConn, err error)) error {
	return c.Close(loop, UDPCloseFunc(fn))
}

// Fd returns the underlying file descriptor.
func (c *UDPConn) Fd() int32 {
	return cxev.UDPFd(&c.udp)
}

// Cleanup releases callback resources.
//
// Call this after [UDPConn.Close] completes or if an error occurs during
// operations. This unregisters any pending callbacks to prevent memory leaks.
func (c *UDPConn) Cleanup() {
	if c.callbackID != 0 {
		cxev.UnregisterUDPCallback(c.callbackID)
		c.callbackID = 0
	}
}

// sockaddrToUDPAddr converts a cxev.Sockaddr to [net.UDPAddr].
//
// The sockaddr is expected to be in BSD format:
//   - offset 0: sin_len (1 byte)
//   - offset 1: sin_family (1 byte, 2 = AF_INET)
//   - offset 2-3: sin_port (2 bytes, big-endian)
//   - offset 4-7: sin_addr (4 bytes)
func sockaddrToUDPAddr(addr *cxev.Sockaddr) *net.UDPAddr {
	family := addr[1]

	if family == 2 { // AF_INET
		port := uint16(addr[2])<<8 | uint16(addr[3])
		ip := net.IPv4(addr[4], addr[5], addr[6], addr[7])
		return &net.UDPAddr{IP: ip, Port: int(port)}
	}

	return nil
}
