/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package rediscli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/crrow/libxev-go/pkg/redisproto"
)

// ErrEmptyCommand indicates no command tokens were provided.
var ErrEmptyCommand = errors.New("empty command")

// Client executes RESP2 commands against a Redis-compatible endpoint.
type Client struct {
	Addr    string
	Timeout time.Duration
	Dial    func(network, addr string) (net.Conn, error)
}

// NewClient creates a client with default TCP dial behavior.
func NewClient(addr string) *Client {
	return &Client{
		Addr:    addr,
		Timeout: 2 * time.Second,
		Dial: func(network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.Dial(network, addr)
		},
	}
}

// Run executes one-shot or interactive mode depending on args.
// If args are empty, it enters interactive mode.
func (c *Client) Run(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) > 0 {
		if err := c.runOneShot(args, out, errOut); err != nil {
			_, _ = fmt.Fprintf(errOut, "redis-cli error: %v\n", err)
			return 1
		}
		return 0
	}

	if err := c.runInteractive(in, out, errOut); err != nil {
		_, _ = fmt.Fprintf(errOut, "redis-cli error: %v\n", err)
		return 1
	}
	return 0
}

func (c *Client) runOneShot(args []string, out, errOut io.Writer) error {
	resp, err := c.Do(args)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, FormatValue(resp))
	if resp.Kind == redisproto.KindError {
		_, _ = fmt.Fprintln(errOut, "server returned an error reply")
	}
	return nil
}

func (c *Client) runInteractive(in io.Reader, out, errOut io.Writer) error {
	_, _ = fmt.Fprintln(out, "redis-cli interactive mode (type 'quit' or 'exit' to leave)")
	scanner := bufio.NewScanner(in)

	for {
		_, _ = fmt.Fprint(out, "redis> ")
		if !scanner.Scan() {
			if scanErr := scanner.Err(); scanErr != nil {
				return scanErr
			}
			return nil
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "quit") || strings.EqualFold(line, "exit") {
			return nil
		}

		args := strings.Fields(line)
		resp, err := c.Do(args)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "redis-cli error: %v\n", err)
			continue
		}
		_, _ = fmt.Fprintln(out, FormatValue(resp))
	}
}

// Do sends a single command and waits for one response frame.
func (c *Client) Do(args []string) (redisproto.Value, error) {
	if len(args) == 0 {
		return redisproto.Value{}, ErrEmptyCommand
	}

	conn, err := c.Dial("tcp", c.Addr)
	if err != nil {
		return redisproto.Value{}, fmt.Errorf("connect %s failed: %w", c.Addr, err)
	}
	defer conn.Close()
	if c.Timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.Timeout))
	}

	cmd := BuildCommand(args)
	wire, err := redisproto.Encode(cmd)
	if err != nil {
		return redisproto.Value{}, fmt.Errorf("encode command failed: %w", err)
	}

	if _, err = conn.Write(wire); err != nil {
		return redisproto.Value{}, fmt.Errorf("write command failed: %w", err)
	}

	resp, err := ReadResponse(conn)
	if err != nil {
		return redisproto.Value{}, err
	}
	return resp, nil
}

// BuildCommand constructs a RESP2 array of bulk strings.
func BuildCommand(args []string) redisproto.Value {
	arr := make([]redisproto.Value, 0, len(args))
	for _, arg := range args {
		arr = append(arr, redisproto.Value{Kind: redisproto.KindBulkString, Bulk: []byte(arg)})
	}
	return redisproto.Value{Kind: redisproto.KindArray, Array: arr}
}

// ReadResponse reads one RESP2 frame from reader.
func ReadResponse(r io.Reader) (redisproto.Value, error) {
	parser := redisproto.NewParser()
	br := bufio.NewReader(r)
	buf := make([]byte, 4096)

	for {
		n, err := br.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return redisproto.Value{}, fmt.Errorf("protocol error: connection closed before response")
			}
			return redisproto.Value{}, fmt.Errorf("read response failed: %w", err)
		}

		frames, parseErr := parser.Feed(buf[:n])
		if parseErr != nil {
			return redisproto.Value{}, fmt.Errorf("protocol error: %w", parseErr)
		}
		if len(frames) > 0 {
			return frames[0], nil
		}
	}
}

// FormatValue renders RESP2 values for CLI output.
func FormatValue(v redisproto.Value) string {
	switch v.Kind {
	case redisproto.KindSimpleString:
		return v.Str
	case redisproto.KindError:
		return "(error) " + v.Str
	case redisproto.KindInteger:
		return fmt.Sprintf("(integer) %d", v.Int)
	case redisproto.KindBulkString:
		return string(v.Bulk)
	case redisproto.KindNull:
		return "(nil)"
	case redisproto.KindArray:
		if len(v.Array) == 0 {
			return "(empty array)"
		}
		var b strings.Builder
		for i, item := range v.Array {
			_, _ = fmt.Fprintf(&b, "%d) %s", i+1, FormatValue(item))
			if i < len(v.Array)-1 {
				_ = b.WriteByte('\n')
			}
		}
		return b.String()
	default:
		return "(unknown)"
	}
}
