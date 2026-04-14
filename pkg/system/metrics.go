package system

import (
  "context"
  "time"

  "github.com/shirou/gopsutil/v4/cpu"
  "github.com/shirou/gopsutil/v4/disk"
  "github.com/shirou/gopsutil/v4/load"
  "github.com/shirou/gopsutil/v4/mem"
  "github.com/shirou/gopsutil/v4/net"
)

// Metrics contains real-time system metrics for heartbeat reporting.
type Metrics struct {
  CPUPercent  float64 `json:"cpu_percent"`
  RAMPercent  float64 `json:"ram_percent"`
  RAMUsedMB   uint64  `json:"ram_used_mb"`
  DiskPercent float64 `json:"disk_percent"`
  DiskUsedGB  uint64  `json:"disk_used_gb"`
  NetRxBytes  uint64  `json:"net_rx_bytes"`
  NetTxBytes  uint64  `json:"net_tx_bytes"`
  Load1m      float64 `json:"load_1m"`
  Load5m      float64 `json:"load_5m"`
}

// CollectMetrics gathers current system metrics for heartbeat reporting.
func CollectMetrics(ctx context.Context) (*Metrics, error) {
  m := &Metrics{}

  // CPU usage (average over 1 second)
  if pct, err := cpu.PercentWithContext(ctx, 1*time.Second, false); err == nil && len(pct) > 0 {
    m.CPUPercent = pct[0]
  }

  // RAM
  if vmem, err := mem.VirtualMemoryWithContext(ctx); err == nil {
    m.RAMPercent = vmem.UsedPercent
    m.RAMUsedMB = vmem.Used / 1024 / 1024
  }

  // Disk (root filesystem)
  if dusage, err := disk.UsageWithContext(ctx, "/"); err == nil {
    m.DiskPercent = dusage.UsedPercent
    m.DiskUsedGB = dusage.Used / 1024 / 1024 / 1024
  }

  // Network I/O (total bytes since boot)
  if counters, err := net.IOCountersWithContext(ctx, false); err == nil && len(counters) > 0 {
    m.NetRxBytes = counters[0].BytesRecv
    m.NetTxBytes = counters[0].BytesSent
  }

  // Load average
  if l, err := load.AvgWithContext(ctx); err == nil {
    m.Load1m = l.Load1
    m.Load5m = l.Load5
  }

  return m, nil
}

// CollectMetricsFast collects metrics without the 1-second CPU sampling.
// Useful for quick status checks.
func CollectMetricsFast(ctx context.Context) (*Metrics, error) {
  m := &Metrics{}

  if vmem, err := mem.VirtualMemoryWithContext(ctx); err == nil {
    m.RAMPercent = vmem.UsedPercent
    m.RAMUsedMB = vmem.Used / 1024 / 1024
  }

  if dusage, err := disk.UsageWithContext(ctx, "/"); err == nil {
    m.DiskPercent = dusage.UsedPercent
    m.DiskUsedGB = dusage.Used / 1024 / 1024 / 1024
  }

  if counters, err := net.IOCountersWithContext(ctx, false); err == nil && len(counters) > 0 {
    m.NetRxBytes = counters[0].BytesRecv
    m.NetTxBytes = counters[0].BytesSent
  }

  if l, err := load.AvgWithContext(ctx); err == nil {
    m.Load1m = l.Load1
    m.Load5m = l.Load5
  }

  return m, nil
}
