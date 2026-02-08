/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package redismvp

import (
	"errors"
	"fmt"
	"net"
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

	stopCh  chan struct{}
	doneCh  chan struct{}
	stopped atomic.Bool
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
		c.close(true)
	}

	for i := 0; i < 32; i++ {
		_ = s.loop.Poll()
	}
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
		client.close(true)
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
		c.close(false)
		return xev.Stop
	}
	if len(data) == 0 {
		c.close(false)
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
		resp := c.execute(frame)
		encoded, encodeErr := redisproto.Encode(resp)
		if encodeErr != nil {
			encoded, _ = redisproto.Encode(redisError("ERR internal encode error"))
		}
		wire = append(wire, encoded...)
	}
	if writeErr := writeAll(c.conn.Fd(), wire); writeErr != nil {
		c.close(false)
		return xev.Stop
	}
	return xev.Continue
}

func (c *clientConn) execute(frame redisproto.Value) redisproto.Value {
	name, args, err := decodeCommand(frame)
	if err != nil {
		return redisError("ERR Protocol error: " + err.Error())
	}

	switch name {
	case "PING":
		if len(args) == 0 {
			return redisSimple("PONG")
		}
		if len(args) == 1 {
			return redisBulk(args[0])
		}
		return wrongArity("ping")
	case "ECHO":
		if len(args) != 1 {
			return wrongArity("echo")
		}
		return redisBulk(args[0])
	case "SET":
		if len(args) != 2 {
			return wrongArity("set")
		}
		c.server.store.Set(args[0], []byte(args[1]))
		return redisSimple("OK")
	case "GET":
		if len(args) != 1 {
			return wrongArity("get")
		}
		v, ok := c.server.store.Get(args[0])
		if !ok {
			return redisNull()
		}
		return redisproto.Value{Kind: redisproto.KindBulkString, Bulk: v}
	case "DEL":
		if len(args) == 0 {
			return wrongArity("del")
		}
		return redisInt(c.server.store.Del(args...))
	case "INCR":
		if len(args) != 1 {
			return wrongArity("incr")
		}
		n, incrErr := c.server.store.Incr(args[0])
		if incrErr != nil {
			if errors.Is(incrErr, errValueNotInteger) {
				return redisError("ERR value is not an integer or out of range")
			}
			return redisError("ERR " + incrErr.Error())
		}
		return redisInt(n)
	default:
		return redisError("ERR unknown command '" + strings.ToLower(name) + "'")
	}
}

func (c *clientConn) writeSyncResponse(v redisproto.Value) xev.Action {
	wire, err := redisproto.Encode(v)
	if err != nil {
		wire, _ = redisproto.Encode(redisError("ERR internal encode error"))
	}
	if writeErr := writeAll(c.conn.Fd(), wire); writeErr != nil {
		c.close(false)
		return xev.Stop
	}
	return xev.Continue
}

func (c *clientConn) close(forceCloseFD bool) {
	if c.closed {
		return
	}
	c.closed = true

	c.server.clientsMu.Lock()
	delete(c.server.clients, c)
	c.server.clientsMu.Unlock()

	if forceCloseFD {
		_ = syscall.Close(int(c.conn.Fd()))
	}
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

func wrongArity(cmd string) redisproto.Value {
	return redisError("ERR wrong number of arguments for '" + cmd + "' command")
}

func redisSimple(s string) redisproto.Value {
	return redisproto.Value{Kind: redisproto.KindSimpleString, Str: s}
}

func redisError(s string) redisproto.Value {
	return redisproto.Value{Kind: redisproto.KindError, Str: s}
}

func redisBulk(s string) redisproto.Value {
	return redisproto.Value{Kind: redisproto.KindBulkString, Bulk: []byte(s)}
}

func redisNull() redisproto.Value {
	return redisproto.Value{Kind: redisproto.KindNull}
}

func redisInt(n int64) redisproto.Value {
	return redisproto.Value{Kind: redisproto.KindInteger, Int: n}
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
