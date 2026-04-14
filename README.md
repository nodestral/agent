# Nodestral Agent

> Lightweight agent for [Nodestral](https://nodestral.io) — VPS fleet management control plane.

One command to install. Auto-registers with your dashboard. Discovers everything running on your server. Zero config.

## Quick Start

```bash
curl -sSL https://nodestral.io/install | sh
```

The agent will:
1. Detect your system info (OS, CPU, RAM, disk, IPs)
2. Identify your cloud provider (Tencent, AWS, GCP, Azure, Hetzner, DigitalOcean)
3. Register with the Nodestral API
4. Start sending heartbeats every 30 seconds
5. Run auto-discovery every 5 minutes

## What It Detects

- **System**: hostname, OS, kernel, CPU, RAM, disk, network
- **Cloud provider**: auto-detected via metadata APIs
- **Services**: running systemd services + versions (nginx, postgresql, redis, etc.)
- **Docker**: containers, images, status
- **Packages**: installed software (nodejs, golang, python3, certbot, etc.)
- **Network**: listening ports + owning processes
- **Security**: SSL certificate expiry, firewall status, pending OS updates, SSH users
- **Monitoring tools**: detects existing node_exporter, Netdata, Datadog agent, etc.

## What It Doesn't Do

- ❌ Modify any existing configs, services, or files
- ❌ Replace or conflict with existing monitoring tools
- ❌ Require Docker or any runtime dependencies
- ❌ Store data locally (all data sent to Nodestral API)

## Architecture

```
┌──────────────────────┐
│   Nodestral Agent    │
│   (Go, single binary)│
├──────────────────────┤
│ · System Collector   │──→ API (heartbeat every 30s)
│ · Provider Detector  │──→ API (discovery every 5min)
│ · Discovery Scanner  │
│ · OTel Manager       │──→ Manages OTel Collector lifecycle
│ · Terminal Bridge    │──→ WebSocket SSH proxy (future)
└──────────────────────┘
```

## Build from Source

Requires Go 1.22+

```bash
git clone https://github.com/nodestral/agent.git
cd agent
go build -ldflags="-s -w" -o nodestral-agent ./cmd/agent/
```

## Configuration

Config file: `/etc/nodestral/agent.yaml`

```yaml
api_url: https://api.nodestral.io
node_id: ""           # auto-generated on first register
auth_token: ""        # provided by API on register
heartbeat_interval: 30s
discovery_interval: 300s
```

## Systemd Service

Installed automatically by the install script:

```bash
systemctl status nodestral-agent
journalctl -u nodestral-agent -f
```

## Tech Stack

- **Go** — single static binary, cross-compiled
- **gopsutil** — system metrics collection
- **YAML** — config file parsing
- No other runtime dependencies

## License

See [LICENSE](LICENSE) file.
