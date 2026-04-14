package discovery

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "log"
  "net/http"
  "os"
  "os/exec"
  "path/filepath"
  "regexp"
  "runtime"
  "strings"
  "time"

  "github.com/nodestral/agent/pkg/config"
)

// Snapshot contains the full discovery result for a node.
type Snapshot struct {
  Services       []ServiceInfo `json:"services"`
  Packages       []PackageInfo `json:"packages"`
  Containers     []ContainerInfo `json:"containers,omitempty"`
  ListeningPorts []PortInfo    `json:"listening_ports"`
  Certificates   []CertInfo    `json:"certificates,omitempty"`
  Firewall       *FirewallInfo `json:"firewall,omitempty"`
  Updates        *UpdateInfo   `json:"updates,omitempty"`
  SSHUsers       []string      `json:"ssh_users"`
  MonitoringTools []string     `json:"monitoring_tools"`
}

// ServiceInfo represents a detected systemd service.
type ServiceInfo struct {
  Name    string `json:"name"`
  Status  string `json:"status"` // running, stopped, failed
  Version string `json:"version,omitempty"`
}

// PackageInfo represents a detected installed package.
type PackageInfo struct {
  Name    string `json:"name"`
  Version string `json:"version"`
}

// ContainerInfo represents a detected Docker container.
type ContainerInfo struct {
  Name    string `json:"name"`
  Image   string `json:"image"`
  Status  string `json:"status"`
  CPUPct  float64 `json:"cpu_pct,omitempty"`
}

// PortInfo represents a listening port.
type PortInfo struct {
  Port    int    `json:"port"`
  Process string `json:"process,omitempty"`
}

// CertInfo represents a detected SSL certificate.
type CertInfo struct {
  Domain    string `json:"domain"`
  Issuer    string `json:"issuer"`
  ExpiresAt string `json:"expires_at"`
}

// FirewallInfo represents firewall status.
type FirewallInfo struct {
  Type  string `json:"type"`
  Status string `json:"status"`
  Rules  int    `json:"rules"`
}

// UpdateInfo represents pending OS updates.
type UpdateInfo struct {
  Pending  int `json:"pending"`
  Critical int `json:"critical"`
}

// Discoverer performs node auto-discovery.
type Discoverer struct {
  cfg    *config.Config
  client *http.Client
}

// New creates a new Discoverer.
func New(cfg *config.Config) *Discoverer {
  return &Discoverer{
    cfg: cfg,
    client: &http.Client{
      Timeout: 15 * time.Second,
    },
  }
}

// Run performs a full discovery scan and reports to the API.
func (d *Discoverer) Run(ctx context.Context) error {
  snapshot := d.scan(ctx)

  payloadData, err := json.Marshal(snapshot)
  if err != nil {
    return fmt.Errorf("marshal discovery: %w", err)
  }

  // Merge node_id into the snapshot JSON
  var merged map[string]interface{}
  if err := json.Unmarshal(payloadData, &merged); err != nil {
    return fmt.Errorf("unmarshal discovery: %w", err)
  }
  merged["node_id"] = d.cfg.NodeID

  data, err := json.Marshal(merged)
  if err != nil {
    return fmt.Errorf("marshal discovery: %w", err)
  }

  req, err := http.NewRequestWithContext(ctx, http.MethodPost,
    d.cfg.APIURL+"/agent/discovery", bytes.NewReader(data))
  if err != nil {
    return fmt.Errorf("create discovery request: %w", err)
  }
  req.Header.Set("Content-Type", "application/json")
  req.Header.Set("Authorization", "Bearer "+d.cfg.AuthToken)

  resp, err := d.client.Do(req)
  if err != nil {
    return fmt.Errorf("send discovery: %w", err)
  }
  defer resp.Body.Close()

  if resp.StatusCode >= 400 {
    body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
    return fmt.Errorf("discovery rejected (status %d): %s", resp.StatusCode, string(body))
  }

  return nil
}

// scan performs the actual discovery on the local system.
func (d *Discoverer) scan(ctx context.Context) *Snapshot {
  s := &Snapshot{}

  s.Services = d.detectServices(ctx)
  s.Packages = d.detectPackages()
  s.Containers = d.detectDockerContainers(ctx)
  s.ListeningPorts = d.detectListeningPorts()
  s.Certificates = d.detectCertificates()
  s.Firewall = d.detectFirewall()
  s.Updates = d.detectUpdates()
  s.SSHUsers = d.detectSSHUsers()
  s.MonitoringTools = d.detectMonitoringTools()

  return s
}

// StartLoop runs discovery periodically, blocking until context is cancelled.
func (d *Discoverer) StartLoop(ctx context.Context) {
  ticker := time.NewTicker(d.cfg.DiscoveryInterval)
  defer ticker.Stop()

  // Run first discovery immediately
  if err := d.Run(ctx); err != nil {
    log.Printf("discovery: %v", err)
  }

  for {
    select {
    case <-ctx.Done():
      return
    case <-ticker.C:
      if err := d.Run(ctx); err != nil {
        log.Printf("discovery: %v", err)
      }
    }
  }
}

func (d *Discoverer) detectServices(ctx context.Context) []ServiceInfo {
  // Notable services to report with version detection
  notableServices := map[string]string{
    "nginx":      "nginx -v 2>&1",
    "postgresql": "psql --version",
    "redis":      "redis-server --version",
    "mysql":      "mysql --version",
    "docker":     "docker --version",
    "mongod":     "mongod --version",
  }

  var services []ServiceInfo

  // Get running systemd services
  out, err := exec.CommandContext(ctx, "systemctl", "list-units",
    "--type=service", "--state=running", "--no-legend", "--no-pager").Output()
  if err == nil {
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    for _, line := range lines {
      fields := strings.Fields(line)
      if len(fields) < 1 {
        continue
      }
      name := strings.TrimSuffix(fields[0], ".service")
      svc := ServiceInfo{Name: name, Status: "running"}

      // Try to get version for notable services
      if cmd, ok := notableServices[name]; ok {
        if ver := getVersion(cmd); ver != "" {
          svc.Version = ver
        }
      }

      services = append(services, svc)
    }
  }

  return services
}

func (d *Discoverer) detectPackages() []PackageInfo {
  // Only check notable packages, not all installed
  notable := []string{
    "nginx", "postgresql", "redis-server", "mysql-server",
    "nodejs", "golang-go", "python3", "certbot",
    "git", "docker.io", "docker-ce",
  }

  var packages []PackageInfo

  switch runtime.GOOS {
  case "linux":
    // Try dpkg first (Debian/Ubuntu)
    if _, err := os.Stat("/usr/bin/dpkg"); err == nil {
      for _, pkg := range notable {
        out, err := exec.Command("dpkg-query", "-W", "-f=${Version}", pkg).Output()
        if err == nil {
          packages = append(packages, PackageInfo{
            Name:    pkg,
            Version: strings.TrimSpace(string(out)),
          })
        }
      }
      return packages
    }
    // Try rpm (RHEL/CentOS)
    if _, err := os.Stat("/usr/bin/rpm"); err == nil {
      for _, pkg := range notable {
        out, err := exec.Command("rpm", "-q", "--queryformat", "%{VERSION}", pkg).Output()
        if err == nil {
          packages = append(packages, PackageInfo{
            Name:    pkg,
            Version: strings.TrimSpace(string(out)),
          })
        }
      }
    }
  }

  return packages
}

func (d *Discoverer) detectDockerContainers(ctx context.Context) []ContainerInfo {
  // Check if docker is available
  if _, err := exec.LookPath("docker"); err != nil {
    return nil
  }

  out, err := exec.CommandContext(ctx, "docker", "ps",
    "--format", `{{.Names}}|{{.Image}}|{{.Status}}`).Output()
  if err != nil {
    return nil
  }

  var containers []ContainerInfo
  lines := strings.Split(strings.TrimSpace(string(out)), "\n")
  for _, line := range lines {
    if line == "" {
      continue
    }
    parts := strings.SplitN(line, "|", 3)
    if len(parts) < 3 {
      continue
    }
    status := "stopped"
    if strings.Contains(parts[2], "Up") {
      status = "running"
    }
    containers = append(containers, ContainerInfo{
      Name:   parts[0],
      Image:  parts[1],
      Status: status,
    })
  }

  return containers
}

func (d *Discoverer) detectListeningPorts() []PortInfo {
  var ports []PortInfo

  // Parse /proc/net/tcp and /proc/net/tcp6
  for _, procFile := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
    data, err := os.ReadFile(procFile)
    if err != nil {
      continue
    }

    lines := strings.Split(strings.TrimSpace(string(data)), "\n")
    for i, line := range lines {
      if i == 0 { // skip header
        continue
      }
      fields := strings.Fields(line)
      if len(fields) < 10 {
        continue
      }

      // State 0A = LISTEN
      if fields[3] != "0A" {
        continue
      }

      // Parse local address (IP:PORT in hex)
      addr := fields[1]
      colonIdx := strings.LastIndex(addr, ":")
      if colonIdx < 0 {
        continue
      }

      var port int
      fmt.Sscanf(addr[colonIdx+1:], "%X", &port)
      if port == 0 {
        continue
      }

      // Get process name from inode
      inode := fields[9]
      procName := findProcessByInode(inode)

      ports = append(ports, PortInfo{
        Port:    port,
        Process: procName,
      })
    }
  }

  return ports
}

func findProcessByInode(inode string) string {
  if inode == "0" {
    return ""
  }

  // Search /proc/*/fd for matching socket inode
  entries, err := os.ReadDir("/proc")
  if err != nil {
    return ""
  }

  for _, entry := range entries {
    if !entry.IsDir() {
      continue
    }
    pid := entry.Name()
    if _, err := fmt.Sscanf(pid, "%d", new(int)); err != nil {
      continue
    }

    fdDir := fmt.Sprintf("/proc/%s/fd", pid)
    fds, err := os.ReadDir(fdDir)
    if err != nil {
      continue
    }

    for _, fd := range fds {
      link, err := os.Readlink(fmt.Sprintf("%s/%s", fdDir, fd.Name()))
      if err != nil {
        continue
      }
      if strings.Contains(link, "socket:["+inode+"]") {
        // Found the process — get its name
        cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%s/comm", pid))
        if err == nil {
          return strings.TrimSpace(string(cmdline))
        }
        return pid
      }
    }
  }

  return ""
}

func (d *Discoverer) detectCertificates() []CertInfo {
  // Common cert locations
  certDirs := []string{
    "/etc/letsencrypt/live",
    "/etc/ssl/certs",
  }

  var certs []CertInfo
  seen := make(map[string]bool)

  for _, dir := range certDirs {
    entries, err := os.ReadDir(dir)
    if err != nil {
      continue
    }
    for _, entry := range entries {
      if !entry.IsDir() {
        continue
      }
      certPath := filepath.Join(dir, entry.Name(), "fullchain.pem")
      if _, err := os.Stat(certPath); err != nil {
        certPath = filepath.Join(dir, entry.Name(), "cert.pem")
        if _, err := os.Stat(certPath); err != nil {
          continue
        }
      }

      out, err := exec.Command("openssl", "x509", "-in", certPath,
        "-noout", "-subject", "-issuer", "-enddate").Output()
      if err != nil {
        continue
      }

      info := parseCertOutput(entry.Name(), string(out))
      if info != nil && !seen[info.Domain] {
        seen[info.Domain] = true
        certs = append(certs, *info)
      }
    }
  }

  return certs
}

func parseCertOutput(domain string, output string) *CertInfo {
  info := &CertInfo{Domain: domain}

  // Parse subject (domain)
  if m := regexp.MustCompile(`subject=.*CN\s*=\s*([^\s,/]+)`).FindStringSubmatch(output); len(m) > 1 {
    info.Domain = m[1]
  }

  // Parse issuer
  if m := regexp.MustCompile(`issuer=.*O\s*=\s*([^\s,/]+)`).FindStringSubmatch(output); len(m) > 1 {
    info.Issuer = m[1]
  }

  // Parse expiry
  if m := regexp.MustCompile(`notAfter=(.+)`).FindStringSubmatch(output); len(m) > 1 {
    info.ExpiresAt = strings.TrimSpace(m[1])
  }

  return info
}

func (d *Discoverer) detectFirewall() *FirewallInfo {
  // Try ufw
  if _, err := exec.LookPath("ufw"); err == nil {
    out, err := exec.Command("ufw", "status").Output()
    if err == nil {
      status := "inactive"
      rules := 0
      if strings.Contains(string(out), "Status: active") {
        status = "active"
        // Count numbered rules
        for _, line := range strings.Split(string(out), "\n") {
          if matched, _ := regexp.MatchString(`^\[\d+\]`, strings.TrimSpace(line)); matched {
            rules++
          }
        }
      }
      return &FirewallInfo{Type: "ufw", Status: status, Rules: rules}
    }
  }

  // Try iptables
  if _, err := exec.LookPath("iptables"); err == nil {
    out, err := exec.Command("iptables", "-L", "-n").Output()
    if err == nil {
      rules := strings.Count(string(out), "\n") - 2 // subtract header lines
      return &FirewallInfo{Type: "iptables", Status: "active", Rules: rules}
    }
  }

  return nil
}

func (d *Discoverer) detectUpdates() *UpdateInfo {
  switch runtime.GOOS {
  case "linux":
    // Try apt (Debian/Ubuntu)
    if _, err := os.Stat("/usr/bin/apt"); err == nil {
      out, err := exec.Command("apt", "list", "--upgradable").Output()
      if err == nil {
        lines := strings.Split(strings.TrimSpace(string(out)), "\n")
        pending := 0
        critical := 0
        for _, line := range lines {
          if strings.Contains(line, "[upgradable") {
            pending++
            if strings.Contains(line, "security") {
              critical++
            }
          }
        }
        return &UpdateInfo{Pending: pending, Critical: critical}
      }
    }
  }
  return nil
}

func (d *Discoverer) detectSSHUsers() []string {
  var users []string

  // Read users with login shells from /etc/passwd
  data, err := os.ReadFile("/etc/passwd")
  if err != nil {
    return users
  }

  loginShells := []string{"/bin/bash", "/bin/zsh", "/bin/sh", "/bin/fish"}
  for _, line := range strings.Split(string(data), "\n") {
    parts := strings.Split(line, ":")
    if len(parts) < 7 {
      continue
    }
    shell := parts[6]
    for _, ls := range loginShells {
      if shell == ls {
        users = append(users, parts[0])
        break
      }
    }
  }

  return users
}

func (d *Discoverer) detectMonitoringTools() []string {
  var tools []string

  toolBins := map[string]string{
    "node_exporter": "node_exporter",
    "netdata":       "netdata",
    "datadog-agent": "datadog-agent",
    "grafana-agent": "grafana-agent",
    "telegraf":      "telegraf",
  }

  for name, bin := range toolBins {
    if _, err := exec.LookPath(bin); err == nil {
      tools = append(tools, name)
      continue
    }
    if _, err := exec.Command("systemctl", "is-active", "--quiet", name+".service").CombinedOutput(); err == nil {
      tools = append(tools, name)
    }
  }

  return tools
}

// getVersion runs a command and extracts a version string from the output.
func getVersion(cmd string) string {
  parts := strings.Fields(cmd)
  if len(parts) < 1 {
    return ""
  }

  out, err := exec.Command(parts[0], parts[1:]...).CombinedOutput()
  if err != nil {
    return ""
  }

  // Extract version number from output
  re := regexp.MustCompile(`(\d+\.\d+[\.\d]*[-\w]*)`)
  matches := re.FindStringSubmatch(string(out))
  if len(matches) > 1 {
    return matches[1]
  }
  return ""
}
