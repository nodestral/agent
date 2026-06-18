package heartbeat

import (
  "bytes"
  "context"
  "encoding/json"
  "io"
  "log"
  "net/http"
  "os/exec"
  "time"

  "github.com/nodestral/agent/pkg/config"
  "github.com/nodestral/agent/pkg/nodeexporter"
  "github.com/nodestral/agent/pkg/selfupdate"
  "github.com/nodestral/agent/pkg/system"
)

// Payload is the heartbeat data sent to the API.
type Payload struct {
  NodeID     string         `json:"node_id"`
  CPUPercent float64        `json:"cpu_percent"`
  RAMPercent float64        `json:"ram_percent"`
  RAMUsedMB  uint64         `json:"ram_used_mb"`
  DiskPercent float64       `json:"disk_percent"`
  DiskUsedGB uint64         `json:"disk_used_gb"`
  NetRxBytes uint64         `json:"net_rx_bytes"`
  NetTxBytes uint64         `json:"net_tx_bytes"`
  Load1m     float64        `json:"load_1m"`
  Load5m     float64        `json:"load_5m"`
  // Provider info (sent every heartbeat, API ignores if empty)
  ProviderName     string `json:"provider_name,omitempty"`
  ProviderRegion   string `json:"provider_region,omitempty"`
  ProviderInstType string `json:"provider_instance_type,omitempty"`
}

// Sender sends periodic heartbeats to the Nodestral API.
type Sender struct {
  cfg       *config.Config
  client    *http.Client
  failCount int
  Exporter  interface { Push(ctx context.Context, m *system.Metrics) }
  providerName     string
  providerRegion   string
  providerInstType string
  agentVersion     string
}

// New creates a new heartbeat sender.
func New(cfg *config.Config, provName, provRegion, provInstType, agentVer string) *Sender {
  return &Sender{
    cfg: cfg,
    client: &http.Client{
      Timeout: 10 * time.Second,
    },
    providerName:     provName,
    providerRegion:   provRegion,
    providerInstType: provInstType,
    agentVersion:     agentVer,
  }
}

// Run starts the heartbeat loop, blocking until context is cancelled.
func (s *Sender) Run(ctx context.Context) {
  ticker := time.NewTicker(s.cfg.HeartbeatInterval)
  defer ticker.Stop()

  // Send first heartbeat immediately
  s.send(ctx)

  for {
    select {
    case <-ctx.Done():
      return
    case <-ticker.C:
      s.send(ctx)
    }
  }
}

func (s *Sender) send(ctx context.Context) {
  metrics, err := system.CollectMetrics(ctx)
  if err != nil {
    log.Printf("heartbeat: collect metrics: %v", err)
    return
  }

  payload := Payload{
    NodeID:           s.cfg.NodeID,
    CPUPercent:       metrics.CPUPercent,
    RAMPercent:       metrics.RAMPercent,
    RAMUsedMB:        metrics.RAMUsedMB,
    DiskPercent:      metrics.DiskPercent,
    DiskUsedGB:       metrics.DiskUsedGB,
    NetRxBytes:       metrics.NetRxBytes,
    NetTxBytes:       metrics.NetTxBytes,
    Load1m:           metrics.Load1m,
    Load5m:           metrics.Load5m,
    ProviderName:     s.providerName,
    ProviderRegion:   s.providerRegion,
    ProviderInstType: s.providerInstType,
  }

  data, err := json.Marshal(payload)
  if err != nil {
    log.Printf("heartbeat: marshal: %v", err)
    return
  }

  reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
  defer cancel()

  req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
    s.cfg.APIURL+"/agent/heartbeat", bytes.NewReader(data))
  if err != nil {
    log.Printf("heartbeat: create request: %v", err)
    return
  }
  req.Header.Set("Content-Type", "application/json")
  req.Header.Set("Authorization", "Bearer "+s.cfg.AuthToken)

  resp, err := s.client.Do(req)
  if err != nil {
    s.failCount++
    log.Printf("heartbeat: send failed (attempt %d): %v", s.failCount, err)
    return
  }
  defer resp.Body.Close()

  if resp.StatusCode >= 400 {
    body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
    s.failCount++
    log.Printf("heartbeat: server error (status %d): %s", resp.StatusCode, string(body))
    return
  }

  // Success — reset fail counter
  if s.failCount > 0 {
    log.Printf("heartbeat: reconnected after %d failures", s.failCount)
    s.failCount = 0
  }

  // Parse full response for self-update check
  bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
  var hbResp struct {
    NodeExporterAction    string `json:"node_exporter_action"`
    LatestAgentVersion    string `json:"latest_agent_version"`
    RestartServiceAction  string `json:"restart_service_action"`
  }
  if json.Unmarshal(bodyBytes, &hbResp) == nil {
    if hbResp.NodeExporterAction != "" {
      s.handleNodeExporterAction(hbResp.NodeExporterAction)
    }
    if hbResp.RestartServiceAction != "" {
      s.handleServiceRestart(hbResp.RestartServiceAction)
    }
    if hbResp.LatestAgentVersion != "" {
      // Run self-update in background (will restart if successful)
      ver := s.agentVersion
      go selfupdate.Check(ver, map[string]interface{}{"latest_agent_version": hbResp.LatestAgentVersion})
    }
  }

  // Push metrics to configured backend
  if s.Exporter != nil {
    s.Exporter.Push(ctx, metrics)
  }
}

func (s *Sender) handleNodeExporterAction(action string) {
  switch action {
  case "install":
    log.Println("node_exporter: install requested by API")
    go func() {
      if err := nodeexporter.Install(); err != nil {
        log.Printf("node_exporter: install failed: %v", err)
      }
    }()
  case "uninstall":
    log.Println("node_exporter: uninstall requested by API")
    go func() {
      if err := nodeexporter.Uninstall(); err != nil {
        log.Printf("node_exporter: uninstall failed: %v", err)
      }
    }()
  }
}

func (s *Sender) handleServiceRestart(service string) {
	log.Printf("service-restart: restart requested for %s", service)
	go func() {
		// Validate service name (prevent injection)
		if len(service) == 0 || len(service) > 255 {
			log.Printf("service-restart: invalid service name %q", service)
			return
		}

		// Try without sudo first
		if err := execCommand("systemctl", "restart", service); err != nil {
			log.Printf("service-restart: restart without sudo failed: %v, trying with sudo", err)
			// Try with sudo
			if err := execCommand("sudo", "systemctl", "restart", service); err != nil {
				log.Printf("service-restart: restart with sudo also failed: %v", err)
				return
			}
		}
		log.Printf("service-restart: %s restarted successfully", service)
	}()
}

func execCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		log.Printf("exec: %s output: %s", name, string(output))
	}
	return err
}
