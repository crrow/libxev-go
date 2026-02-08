/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package redismvp

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/crrow/libxev-go/pkg/redisproto"
	"github.com/crrow/libxev-go/pkg/xev"
)

// Server is a Redis-compatible MVP server backed by xev.
type Server struct {
	loop     *xev.Loop
	listener *xev.TCPListener
	store    *Store
	host     string

	clientsMu sync.Mutex
	clients   map[*clientConn]struct{}

	closeMu    sync.Mutex
	pendingFDs []int32
	stopCh     chan struct{}
	doneCh     chan struct{}
	stopped    atomic.Bool
}

// Start creates and runs a server bound to addr.
// Use 127.0.0.1:0 to allocate an ephemeral port.
func Start(addr string) (*Server, error) {
	loop, err := xev.NewLoop()
	if err != nil {
		return nil, err
	}

	listener, err := xev.Listen("tcp", addr)
	if err != nil {
		loop.Close()
		return nil, err
	}

	s := &Server{
		loop:     loop,
		listener: listener,
		store:    NewStore(),
		clients:  make(map[*clientConn]struct{}),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		host:     parseHost(addr),
	}

	if err := s.listener.AcceptFunc(s.loop, s.onAccept); err != nil {
		s.listener.Close()
		s.loop.Close()
		return nil, err
	}

	go s.run()
	return s, nil
}

func (s *Server) run() {
	defer close(s.doneCh)

	for {
		select {
		case <-s.stopCh:
			s.shutdownInLoop()
			return
		default:
		}

		_ = s.loop.Poll()
		s.flushPendingFDs()
		time.Sleep(50 * time.Microsecond)
	}
}

func (s *Server) shutdownInLoop() {
	s.listener.Close()

	s.clientsMu.Lock()
	clients := make([]*clientConn, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsMu.Unlock()

	for _, c := range clients {
		c.shutdown()
	}

	for i := 0; i < 32; i++ {
		_ = s.loop.Poll()
		s.flushPendingFDs()
	}
	for _, c := range clients {
		_ = syscall.Close(int(c.conn.Fd()))
	}
	s.flushPendingFDs()
	s.loop.Close()
}

func (s *Server) onAccept(_ *xev.TCPListener, conn *xev.TCPConn, err error) xev.Action {
	if err != nil {
		return xev.Continue
	}

	client := &clientConn{
		server: s,
		conn:   conn,
		parser: redisproto.NewParser(),
		read:   make([]byte, 4096),
	}

	s.clientsMu.Lock()
	s.clients[client] = struct{}{}
	s.clientsMu.Unlock()

	if readErr := conn.ReadFunc(s.loop, client.read, client.onRead); readErr != nil {
		client.close()
	}
	return xev.Continue
}

// Addr returns listener address host:port.
func (s *Server) Addr() string {
	_, port := s.listener.Addr()
	return fmt.Sprintf("%s:%d", s.host, port)
}

// Close shuts down the server.
func (s *Server) Close() error {
	if !s.stopped.CompareAndSwap(false, true) {
		return nil
	}
	close(s.stopCh)
	<-s.doneCh
	return nil
}

type clientConn struct {
	server *Server
	conn   *xev.TCPConn
	parser *redisproto.Parser
	read   []byte
	closed bool
}

func (c *clientConn) onRead(_ *xev.TCPConn, data []byte, err error) xev.Action {
	if c.closed {
		return xev.Stop
	}
	if err != nil {
		c.close()
		return xev.Stop
	}
	if len(data) == 0 {
		c.close()
		return xev.Stop
	}

	frames, parseErr := c.parser.Feed(data)
	if parseErr != nil {
		return c.writeSyncResponse(redisError("ERR Protocol error: " + parseErr.Error()))
	}

	if len(frames) == 0 {
		return xev.Continue
	}

	wire := make([]byte, 0, 128)
	for _, frame := range frames {
		wire = c.appendResponse(wire, frame)
	}
	if writeErr := writeAll(c.conn.Fd(), wire); writeErr != nil {
		c.close()
		return xev.Stop
	}
	return xev.Continue
}

func (c *clientConn) appendResponse(dst []byte, frame redisproto.Value) []byte {
	if frame.Kind != redisproto.KindArray {
		return appendError(dst, "ERR Protocol error: command must be array")
	}
	if len(frame.Array) == 0 {
		return appendError(dst, "ERR Protocol error: empty command")
	}

	command, ok := tokenBytes(frame.Array[0])
	if !ok {
		return appendError(dst, fmt.Sprintf("ERR Protocol error: invalid command token kind %s", frame.Array[0].Kind))
	}

	switch {
	case commandIs(command, "PING"):
		if len(frame.Array) == 1 {
			return appendSimple(dst, "PONG")
		}
		if len(frame.Array) == 2 {
			arg, ok := tokenBytes(frame.Array[1])
			if !ok {
				return appendError(dst, fmt.Sprintf("ERR Protocol error: invalid command token kind %s", frame.Array[1].Kind))
			}
			return appendBulk(dst, arg)
		}
		return appendWrongArity(dst, "ping")
	case commandIs(command, "ECHO"):
		if len(frame.Array) != 2 {
			return appendWrongArity(dst, "echo")
		}
		arg, ok := tokenBytes(frame.Array[1])
		if !ok {
			return appendError(dst, fmt.Sprintf("ERR Protocol error: invalid command token kind %s", frame.Array[1].Kind))
		}
		return appendBulk(dst, arg)
	case commandIs(command, "SET"):
		if len(frame.Array) != 3 {
			return appendWrongArity(dst, "set")
		}
		key, ok := tokenString(frame.Array[1])
		if !ok {
			return appendError(dst, fmt.Sprintf("ERR Protocol error: invalid command token kind %s", frame.Array[1].Kind))
		}
		value, ok := tokenBytes(frame.Array[2])
		if !ok {
			return appendError(dst, fmt.Sprintf("ERR Protocol error: invalid command token kind %s", frame.Array[2].Kind))
		}
		c.server.store.Set(key, value)
		return appendSimple(dst, "OK")
	case commandIs(command, "GET"):
		if len(frame.Array) != 2 {
			return appendWrongArity(dst, "get")
		}
		key, ok := tokenString(frame.Array[1])
		if !ok {
			return appendError(dst, fmt.Sprintf("ERR Protocol error: invalid command token kind %s", frame.Array[1].Kind))
		}
		v, hit := c.server.store.Get(key)
		if !hit {
			return appendNull(dst)
		}
		return appendBulk(dst, v)
	case commandIs(command, "DEL"):
		if len(frame.Array) < 2 {
			return appendWrongArity(dst, "del")
		}
		keys := make([]string, 0, len(frame.Array)-1)
		for i := 1; i < len(frame.Array); i++ {
			key, ok := tokenString(frame.Array[i])
			if !ok {
				return appendError(dst, fmt.Sprintf("ERR Protocol error: invalid command token kind %s", frame.Array[i].Kind))
			}
			keys = append(keys, key)
		}
		return appendInteger(dst, c.server.store.Del(keys...))
	case commandIs(command, "INCR"):
		if len(frame.Array) != 2 {
			return appendWrongArity(dst, "incr")
		}
		key, ok := tokenString(frame.Array[1])
		if !ok {
			return appendError(dst, fmt.Sprintf("ERR Protocol error: invalid command token kind %s", frame.Array[1].Kind))
		}
		n, incrErr := c.server.store.Incr(key)
		if incrErr != nil {
			if errors.Is(incrErr, errValueNotInteger) {
				return appendError(dst, "ERR value is not an integer or out of range")
			}
			return appendError(dst, "ERR "+incrErr.Error())
		}
		return appendInteger(dst, n)
	default:
		return appendError(dst, "ERR unknown command '"+strings.ToLower(string(command))+"'")
	}
}

func (c *clientConn) writeSyncResponse(v redisproto.Value) xev.Action {
	wire, err := redisproto.Encode(v)
	if err != nil {
		wire, _ = redisproto.Encode(redisError("ERR internal encode error"))
	}
	if writeErr := writeAll(c.conn.Fd(), wire); writeErr != nil {
		c.close()
		return xev.Stop
	}
	return xev.Continue
}

func (c *clientConn) close() {
	if c.closed {
		return
	}
	c.closed = true

	c.server.clientsMu.Lock()
	delete(c.server.clients, c)
	c.server.clientsMu.Unlock()

	c.server.enqueueFD(c.conn.Fd())
}

func (c *clientConn) shutdown() {
	if c.closed {
		return
	}
	c.closed = true

	c.server.clientsMu.Lock()
	delete(c.server.clients, c)
	c.server.clientsMu.Unlock()

	_ = syscall.Shutdown(int(c.conn.Fd()), syscall.SHUT_RDWR)
}

func (s *Server) enqueueFD(fd int32) {
	s.closeMu.Lock()
	s.pendingFDs = append(s.pendingFDs, fd)
	s.closeMu.Unlock()
}

func (s *Server) flushPendingFDs() {
	s.closeMu.Lock()
	pending := s.pendingFDs
	if len(pending) > 0 {
		s.pendingFDs = nil
	}
	s.closeMu.Unlock()

	for _, fd := range pending {
		_ = syscall.Close(int(fd))
	}
}

func tokenBytes(v redisproto.Value) ([]byte, bool) {
	switch v.Kind {
	case redisproto.KindBulkString:
		return v.Bulk, true
	case redisproto.KindSimpleString:
		return []byte(v.Str), true
	default:
		return nil, false
	}
}

func tokenString(v redisproto.Value) (string, bool) {
	switch v.Kind {
	case redisproto.KindBulkString:
		return string(v.Bulk), true
	case redisproto.KindSimpleString:
		return v.Str, true
	default:
		return "", false
	}
}

func commandIs(token []byte, name string) bool {
	if len(token) != len(name) {
		return false
	}
	for i := 0; i < len(token); i++ {
		b := token[i]
		if b >= 'a' && b <= 'z' {
			b -= 'a' - 'A'
		}
		if b != name[i] {
			return false
		}
	}
	return true
}

func appendSimple(dst []byte, s string) []byte {
	dst = append(dst, '+')
	dst = append(dst, s...)
	return append(dst, '\r', '\n')
}

func appendError(dst []byte, s string) []byte {
	dst = append(dst, '-')
	dst = append(dst, s...)
	return append(dst, '\r', '\n')
}

func appendBulk(dst, bulk []byte) []byte {
	dst = append(dst, '$')
	dst = strconv.AppendInt(dst, int64(len(bulk)), 10)
	dst = append(dst, '\r', '\n')
	dst = append(dst, bulk...)
	return append(dst, '\r', '\n')
}

func appendNull(dst []byte) []byte {
	return append(dst, '$', '-', '1', '\r', '\n')
}

func appendInteger(dst []byte, n int64) []byte {
	dst = append(dst, ':')
	dst = strconv.AppendInt(dst, n, 10)
	return append(dst, '\r', '\n')
}

func appendWrongArity(dst []byte, cmd string) []byte {
	return appendError(dst, "ERR wrong number of arguments for '"+cmd+"' command")
}

func decodeCommand(frame redisproto.Value) (string, []string, error) {
	if frame.Kind != redisproto.KindArray {
		return "", nil, errors.New("command must be array")
	}
	if len(frame.Array) == 0 {
		return "", nil, errors.New("empty command")
	}

	parts := make([]string, 0, len(frame.Array))
	for _, item := range frame.Array {
		switch item.Kind {
		case redisproto.KindBulkString:
			parts = append(parts, string(item.Bulk))
		case redisproto.KindSimpleString:
			parts = append(parts, item.Str)
		default:
			return "", nil, fmt.Errorf("invalid command token kind %s", item.Kind)
		}
	}

	name := strings.ToUpper(parts[0])
	return name, parts[1:], nil
}

func redisError(s string) redisproto.Value {
	return redisproto.Value{Kind: redisproto.KindError, Str: s}
}

func parseHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" || host == "0.0.0.0" {
		return "127.0.0.1"
	}
	return host
}

func writeAll(fd int32, payload []byte) error {
	for len(payload) > 0 {
		n, err := syscall.Write(int(fd), payload)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return err
		}
		if n <= 0 {
			return errors.New("short write to socket")
		}
		payload = payload[n:]
	}
	return nil
}
