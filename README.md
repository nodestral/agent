# Nodestral Agent

Lightweight fleet monitoring agent. Single binary, zero runtime dependencies, <10MB.

Collects system metrics (CPU, RAM, disk, network), discovers services, ports, packages, containers, certificates, and firewall status. Reports to a Nodestral backend.

## Quick Start (SaaS)

```bash
curl -sfL https://nodestral.web.id/install.sh | sh -s <install-token>
```

## Building from Source

Requires Go 1.22+.

```bash
git clone https://github.com/nodestral/agent.git
cd agent
go mod download
go build -o nodestral-agent ./cmd/agent/
```

### Cross-compile

```bash
# Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o nodestral-agent-linux_amd64 ./cmd/agent/
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o nodestral-agent-linux_arm64 ./cmd/agent/

# macOS
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o nodestral-agent-darwin_amd64 ./cmd/agent/
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o nodestral-agent-darwin_arm64 ./cmd/agent/
```

## Configuration

Config file: `/etc/nodestral/agent.yaml` (auto-created on first run).

```yaml
# Backend endpoint — change this to your self-hosted backend URL
api_url: https://nx.nodestral.web.id

heartbeat_interval: 30s
discovery_interval: 5m

# Discovery features — see PERMISSIONS.md for details
discovery:
  # Basic features (work with no special permissions)
  services: true
  packages: true
  ports: true
  ssh_users: true

  # Elevated features (off by default — require additional permissions)
  containers: false    # needs: docker group or root
  certificates: false  # needs: read access to /etc/letsencrypt/live/
  firewall: false      # needs: root or sudoers rule
  os_updates: false    # needs: root or sudoers rule
```

## Running with Community Backend

### 1. Start the backend

```bash
git clone https://github.com/nodestral/backend.git
cd backend
go build -o nodestral-backend ./cmd/server/

# Required for production
export JWT_SECRET=your-random-secret

# Start (default port 8080)
./nodestral-backend
```

### 2. Configure the agent

Create `/etc/nodestral/agent.yaml`:

```yaml
api_url: http://localhost:8080
discovery:
  services: true
  packages: true
  ports: true
  ssh_users: true
```

### 3. Run the agent

```bash
sudo mkdir -p /etc/nodestral
sudo ./nodestral-agent
```

On first run, the agent registers itself and the node appears in the dashboard under "Unclaimed Nodes" where you can claim it.

## Registration Methods

### Option A: Open Registration (default)

The agent registers directly. The node has no owner and appears under "Unclaimed Nodes" in the dashboard. Claim it from the UI.

```bash
./nodestral-agent
```

### Option B: Install Token (recommended for production)

The node registers directly owned by the token creator. No claiming step needed.

1. Generate a token via the API:
```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"yourpass"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")

curl -X POST http://localhost:8080/install-tokens \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"max_uses": 2}'
```

2. Register the agent with the token:
```bash
curl -X POST http://localhost:8080/agent/register/token \
  -H "X-Install-Token: <token-from-above>" \
  -H "Content-Type: application/json" \
  -d '{"system":{"hostname":"my-server"},"provider":{"name":"generic"}}'
```

## Discovery Permissions

The agent is designed to run with **minimal privileges**. Elevated discovery features are disabled by default and must be explicitly enabled.

See [PERMISSIONS.md](./PERMISSIONS.md) for the full setup guide.

## Flags

```
-config string
    Path to config file (default: /etc/nodestral/agent.yaml)
```

## Architecture

```
cmd/agent/         Entry point
pkg/config/        Config loading and saving
pkg/register/      Agent registration with backend
pkg/heartbeat/     System metrics collection and reporting
pkg/discovery/     System discovery (services, ports, packages, etc.)
pkg/provider/      Cloud provider detection (AWS, GCP, Tencent, etc.)
pkg/system/        System info collection (hostname, OS, CPU, RAM, disk)
```

## License

MIT
