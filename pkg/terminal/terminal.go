package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nodestral/agent/pkg/config"
)

// Message is the wire format for all WebSocket messages.
type Message struct {
	Type     string `json:"type"`      // "exec","output","error","exit","ping","pong"
	Data     string `json:"data"`
	ExitCode int    `json:"exit_code"`
}

// Client connects to the nx relay via reverse WebSocket.
// The agent connects TO the relay, browser connects TO the relay,
// and the relay bridges them. The agent never opens a port.
type Client struct {
	cfg  *config.Config
	conn *websocket.Conn
	mu   sync.Mutex
}

func New(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

// StartLoop connects to the relay and maintains the connection.
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

		// Read loop — relay sends us commands from the browser
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

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

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
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}

		var m Message
		if err := json.Unmarshal(msg, &m); err != nil {
			c.send(Message{Type: "error", Data: "invalid message format"})
			continue
		}

		switch m.Type {
		case "exec":
			c.handleExec(m.Data)
		case "ping":
			c.send(Message{Type: "pong"})
		}
	}
}

// allowedPrefixes — commands the agent is allowed to execute.
var allowedPrefixes = []string{
	"ls", "cat", "head", "tail", "less", "more", "grep", "find", "wc", "sort", "uniq",
	"df", "du", "free", "top", "htop", "ps", "pgrep", "uptime", "whoami", "id",
	"uname", "hostname", "date", "timedatectl",
	"ip", "ss", "netstat", "ping", "traceroute", "dig", "nslookup", "curl",
	"systemctl", "journalctl", "service",
	"docker", "podman",
	"apt", "apt-get", "dpkg",
	"env", "printenv", "echo",
	"stat", "file", "which", "whereis",
	"mkdir", "touch", "cp", "mv", "rm", "chmod", "chown", "ln",
	"tar", "gzip", "gunzip",
	"openssl", "certbot",
	"crontab",
	"python3", "node", "go", "gcc",
}

var blockedPatterns = []string{
	"rm -rf /", "rm -rf /*", "mkfs", "dd if=", "shutdown", "reboot",
	"poweroff", "halt", "init 0", "init 6",
}

func isCommandAllowed(cmd string) bool {
	for _, p := range blockedPatterns {
		if strings.Contains(cmd, p) {
			return false
		}
	}
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	base := parts[0]
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	for _, prefix := range allowedPrefixes {
		if base == prefix || strings.HasPrefix(base, prefix) {
			return true
		}
	}
	return false
}

func (c *Client) handleExec(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return
	}

	if !isCommandAllowed(cmd) {
		c.send(Message{Type: "error", Data: fmt.Sprintf("command not allowed: %s", cmd)})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shell := exec.CommandContext(ctx, "bash", "-c", cmd)

	stdout, err := shell.StdoutPipe()
	if err != nil {
		c.send(Message{Type: "error", Data: err.Error()})
		return
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		c.send(Message{Type: "error", Data: err.Error()})
		return
	}

	if err := shell.Start(); err != nil {
		c.send(Message{Type: "error", Data: err.Error()})
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)

	stream := func(r io.Reader, msgType string) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				c.send(Message{Type: msgType, Data: string(buf[:n])})
			}
			if err != nil {
				break
			}
		}
	}

	go stream(stdout, "output")
	go stream(stderr, "error")

	wg.Wait()
	err = shell.Wait()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			c.send(Message{Type: "error", Data: err.Error()})
		}
	}

	c.send(Message{Type: "exit", ExitCode: exitCode})
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
