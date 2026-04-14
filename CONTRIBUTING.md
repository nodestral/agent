# Contributing to Nodestral Agent

Thanks for your interest in contributing! This guide covers the basics.

## Getting Started

### Prerequisites

- Go 1.22+
- Make (optional, for convenience)

### Setup

```bash
git clone https://github.com/nodestral/agent.git
cd agent
go mod download
go build ./cmd/agent/
```

### Run locally (development)

```bash
go run ./cmd/agent/
```

## Project Structure

```
agent/
├── cmd/agent/         # Entry point
├── pkg/
│   ├── config/        # YAML config loading
│   ├── system/        # System info & metrics collection
│   ├── provider/      # Cloud provider auto-detection
│   ├── heartbeat/     # Heartbeat loop to API
│   ├── discovery/     # Node auto-discovery scanner
│   ├── register/      # First-time registration with API
│   └── terminal/      # WebSocket SSH bridge (stub)
├── scripts/
│   └── install.sh     # One-command install script
├── go.mod
└── go.sum
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Add doc comments to exported types and functions
- Keep functions focused and small
- Handle errors explicitly — don't swallow them
- Log with `log.Printf` for agent-level messages

## Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Make your changes
4. Run `go vet ./...` and `go build ./cmd/agent/` — ensure no errors
5. Add tests for new functionality
6. Commit with clear messages (`feat: add Docker container detection`)
7. Push and open a Pull Request

### Commit Message Convention

- `feat:` — new feature
- `fix:` — bug fix
- `refactor:` — code restructuring
- `docs:` — documentation changes
- `chore:` — tooling, deps, etc.

## Reporting Issues

Use [GitHub Issues](https://github.com/nodestral/agent/issues) with:
- OS and version
- Go version
- Agent version (`nodestral-agent --version`)
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs (`journalctl -u nodestral-agent`)

## Adding a New Discovery Module

1. Add detection logic in `pkg/discovery/discovery.go`
2. Add corresponding types in the API (nodestral/api)
3. Add frontend rendering (nodestral/web)
4. Write tests
5. Update this README if it's a user-facing feature

## Principles

- **Never modify** the host system — only read
- **Zero dependencies** — single static binary
- **Fail gracefully** — partial discovery is better than crashing
- **Minimal resource usage** — agent should be invisible on the host
- **Privacy first** — only collect what's necessary
