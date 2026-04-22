package heartbeat

import (
  "bytes"
  "context"
  "encoding/json"
  "io"
  "log"
  "net/http"
  "time"

  "github.com/nodestral/agent/pkg/config"
  "github.com/nodestral/agent/pkg/nodeexporter"
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
}

// Sender sends periodic heartbeats to the Nodestral API.
type Sender struct {
  cfg       *config.Config
  client    *http.Client
  failCount int
  Exporter  interface { Push(ctx context.Context, m *system.Metrics) }
}

// New creates a new heartbeat sender.
func New(cfg *config.Config) *Sender {
  return &Sender{
    cfg: cfg,
    client: &http.Client{
      Timeout: 10 * time.Second,
    },
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
    NodeID:      s.cfg.NodeID,
    CPUPercent:  metrics.CPUPercent,
    RAMPercent:  metrics.RAMPercent,
    RAMUsedMB:   metrics.RAMUsedMB,
    DiskPercent: metrics.DiskPercent,
    DiskUsedGB:  metrics.DiskUsedGB,
    NetRxBytes:  metrics.NetRxBytes,
    NetTxBytes:  metrics.NetTxBytes,
    Load1m:      metrics.Load1m,
    Load5m:      metrics.Load5m,
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

  // Check for node_exporter action from API
  var hbResp struct {
    NodeExporterAction string `json:"node_exporter_action"`
  }
  if json.NewDecoder(resp.Body).Decode(&hbResp) == nil && hbResp.NodeExporterAction != "" {
    s.handleNodeExporterAction(hbResp.NodeExporterAction)
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
