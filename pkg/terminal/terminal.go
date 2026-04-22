package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/nodestral/agent/pkg/config"
)

// Message is the wire format for all WebSocket messages.
type Message struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	ExitCode int    `json:"exit_code"`
	Columns  int    `json:"columns"`
	Rows     int    `json:"rows"`
}

// Client connects to the nx relay via reverse WebSocket.
type Client struct {
	cfg  *config.Config
	conn *websocket.Conn
	mu   sync.Mutex
}

func New(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) StartLoop(ctx context.Context) {
	if !c.cfg.Terminal.Enabled {
		log.Println("terminal: disabled (terminal.enabled = false)")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.connect(ctx)
		if err != nil {
			log.Printf("terminal: connect failed: %v (retrying in 10s)", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
				continue
			}
		}

		err = c.readLoop(ctx)
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		log.Printf("terminal: disconnected (retrying in 5s)")

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	wsURL := c.cfg.RelayURL + "/ws"

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.cfg.AuthToken)
	header.Set("X-Node-ID", c.cfg.NodeID)

	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return fmt.Errorf("dial %s: %w", wsURL, err)
	}
	c.conn = conn
	log.Printf("terminal: connected to relay at %s", wsURL)
	return nil
}

func (c *Client) readLoop(ctx context.Context) error {
	var (
		shellCmd *exec.Cmd
		shellIn  io.WriteCloser
		shellMu  sync.Mutex
		active   bool
		done     chan struct{}
	)

	// Start a persistent bash session
	startShell := func() error {
		shellMu.Lock()
		defer shellMu.Unlock()

		if shellCmd != nil && shellCmd.Process != nil {
			shellCmd.Process.Kill()
			shellCmd.Wait()
		}

		// Use interactive bash with line editing disabled for pipe mode
		shellCmd = exec.Command("bash", "--norc", "--noprofile")
		shellCmd.Dir = c.cfg.HomeDir
		if shellCmd.Dir == "" {
			shellCmd.Dir = "/opt/nodestral/agent"
		}
		shellCmd.Env = append([]string{
			"TERM=dumb",
			"PS1=nodestral $ ",
		}, shellEnviron()...)

		var err error
		shellIn, err = shellCmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("stdin pipe: %w", err)
		}

		stdout, err := shellCmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("stdout pipe: %w", err)
		}

		stderr, err := shellCmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("stderr pipe: %w", err)
		}

		if err := shellCmd.Start(); err != nil {
			return fmt.Errorf("shell start: %w", err)
		}

		active = true
		done = make(chan struct{})

		// Stream stdout
		go func() {
			defer close(done)
			buf := make([]byte, 8192)
			for {
				n, err := stdout.Read(buf)
				if n > 0 {
					c.send(Message{Type: "output", Data: sanitizeOutput(buf[:n])})
				}
				if err != nil {
					return
				}
			}
		}()

		// Stream stderr
		go func() {
			buf := make([]byte, 8192)
			for {
				n, err := stderr.Read(buf)
				if n > 0 {
					c.send(Message{Type: "output", Data: sanitizeOutput(buf[:n])})
				}
				if err != nil {
					return
				}
			}
		}()

		// Wait for shell exit
		go func() {
			shellCmd.Wait()
			c.send(Message{Type: "exit", ExitCode: 0})
			shellMu.Lock()
			active = false
			shellMu.Unlock()
		}()

		return nil
	}

	writeShell := func(data string) error {
		shellMu.Lock()
		defer shellMu.Unlock()
		if !active || shellIn == nil {
			return fmt.Errorf("no active shell")
		}
		_, err := fmt.Fprint(shellIn, data)
		return err
	}

	for {
		select {
		case <-ctx.Done():
			shellMu.Lock()
			if shellCmd != nil && shellCmd.Process != nil {
				shellCmd.Process.Kill()
			}
			shellMu.Unlock()
			return nil
		default:
		}

		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}

		var m Message
		if err := json.Unmarshal(msg, &m); err != nil {
			continue
		}

		switch m.Type {
		case "start_shell":
			if err := startShell(); err != nil {
				c.send(Message{Type: "error", Data: err.Error()})
			} else {
				time.Sleep(200 * time.Millisecond) // let shell initialize
			}

		case "exec":
			if !active {
				if err := startShell(); err != nil {
					c.send(Message{Type: "error", Data: err.Error()})
					continue
				}
				time.Sleep(200 * time.Millisecond)
			}
			cmd := m.Data
			if !strings.HasSuffix(cmd, "\n") {
				cmd += "\n"
			}
			if err := writeShell(cmd); err != nil {
				c.send(Message{Type: "error", Data: err.Error()})
			}

		case "input":
			if !active {
				continue
			}
			if err := writeShell(m.Data); err != nil {
				c.send(Message{Type: "error", Data: err.Error()})
			}

		case "resize":
			// No-op with pipes (PTY needed for resize)

		case "ping":
			c.send(Message{Type: "pong"})
		}
	}
}

// shellEnviron returns a clean environment for the shell.
func shellEnviron() []string {
	// Inherit PATH, HOME, LANG from agent process
	var env []string
	for _, e := range []string{"PATH", "HOME", "LANG", "USER"} {
		if v := os.Getenv(e); v != "" {
			env = append(env, e+"="+v)
		}
	}
	return env
}

func sanitizeOutput(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			b.WriteByte('?')
		} else {
			b.WriteRune(r)
		}
		data = data[size:]
	}
	return b.String()
}

func (c *Client) send(m Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	data, _ := json.Marshal(m)
	c.conn.WriteMessage(websocket.TextMessage, data)
}
