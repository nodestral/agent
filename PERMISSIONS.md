# Agent Permissions Guide

The Nodestral agent is designed to run with **minimal privileges** by default. Discovery features that require elevated access are **disabled by default** and must be explicitly enabled.

## Default Behavior (No Special Permissions)

These features work out of the box with a standard unprivileged user:

| Feature | What It Collects | How |
|---------|-----------------|-----|
| **Heartbeat** | CPU, RAM, disk, network, load | `/proc/*`, syscall |
| **Services** | Running systemd services | `systemctl list-units` (public info) |
| **Packages** | Notable installed packages + versions | `dpkg-query` (public info) |
| **Ports** | Listening TCP ports | `/proc/net/tcp`, `/proc/net/tcp6` |
| **SSH Users** | Users with login shells | `/etc/passwd` (world-readable) |
| **Monitoring** | Detected monitoring tools | `which`/`systemctl is-active` |

**No special permissions needed.** The agent works immediately after install.

---

## Opt-In Features (Require Additional Permissions)

These features are **disabled by default**. Enable them in `/etc/nodestral/agent.yaml`:

```yaml
discovery:
  containers: true      # Docker containers
  certificates: true    # SSL/TLS certificates
  firewall: true        # Firewall status and rules
  os_updates: true      # Pending OS security updates
```

### containers: true — Docker Containers

**Collects:** Running containers (name, image, status)

**Permission needed:**
```bash
sudo usermod -aG docker nodestral
```
Then restart the agent: `sudo systemctl restart nodestral-agent`

**Or via sudoers (without group membership):**
```bash
echo "nodestral ALL=(root) NOPASSWD: /usr/bin/docker ps --format *" | sudo tee /etc/sudoers.d/nodestral-docker
```

---

### certificates: true — SSL/TLS Certificates

**Collects:** Certificate domains, issuers, expiry dates

**Permission needed (Let's Encrypt):**
```bash
sudo chmod 755 /etc/letsencrypt/live
sudo chmod 755 /etc/letsencrypt/archive
```

**For other cert locations**, ensure the agent user can read the directory and `.pem` files.

**Verification:**
```bash
sudo -u nodestral openssl x509 -in /etc/letsencrypt/live/yourdomain/fullchain.pem -noout -subject
```

---

### firewall: true — Firewall Status

**Collects:** Firewall type (ufw/iptables), active status, rule count

**Permission needed:**
```bash
echo "nodestral ALL=(root) NOPASSWD: /usr/sbin/ufw status" | sudo tee /etc/sudoers.d/nodestral-firewall
# Or for iptables:
echo "nodestral ALL=(root) NOPASSWD: /usr/sbin/iptables -L -n" | sudo tee -a /etc/sudoers.d/nodestral-firewall
sudo chmod 440 /etc/sudoers.d/nodestral-firewall
```

**Note:** The agent calls these commands directly. If using sudoers, the agent must be patched to use `sudo` for these specific commands. Alternatively, run the agent as root (see below).

---

### os_updates: true — Pending OS Updates

**Collects:** Number of pending updates, count of security/critical updates

**Permission needed:**
```bash
echo "nodestral ALL=(root) NOPASSWD: /usr/bin/apt list --upgradable" | sudo tee /etc/sudoers.d/nodestral-updates
sudo chmod 440 /etc/sudoers.d/nodestral-updates
```

---

## Running as Root (Not Recommended)

The agent can run as root, which grants access to all features automatically:

```bash
# In /etc/systemd/system/nodestral-agent.service
[Service]
User=root
ExecStart=/usr/local/bin/nodestral-agent
```

However, this is **not recommended** for production. A compromised agent with root access is a significant security risk. Use the permission-specific approaches above instead.

---

## Permission Quick Setup

For all features on a fresh Ubuntu/Debian system:

```bash
# Create agent user
sudo useradd -r -s /usr/sbin/nologin -d /etc/nodestral nodestral

# Certificate access
sudo chmod 755 /etc/letsencrypt/live /etc/letsencrypt/archive

# Docker access
sudo usermod -aG docker nodestral

# Sudoers for firewall and updates
sudo tee /etc/sudoers.d/nodestral-agent << 'EOF'
nodestral ALL=(root) NOPASSWD: /usr/sbin/ufw status, /usr/sbin/iptables -L -n, /usr/bin/apt list --upgradable
EOF
sudo chmod 440 /etc/sudoers.d/nodestral-agent

# Enable all discovery features
sudo tee -a /etc/nodestral/agent.yaml << 'EOF'
discovery:
  containers: true
  certificates: true
  firewall: true
  os_updates: true
EOF
```

---

## What the Agent Will NEVER Do

- Read file contents (only reads metadata — certs, package versions)
- Execute arbitrary commands
- Modify system configuration
- Access user data or home directories
- Store credentials on disk beyond its own config token
- Make outbound connections except to the configured API URL
