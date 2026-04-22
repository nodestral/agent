package main

import (
  "context"
  "flag"
  "log"
  "os"
  "os/signal"
  "syscall"

  "github.com/nodestral/agent/pkg/config"
  "github.com/nodestral/agent/pkg/discovery"
  "github.com/nodestral/agent/pkg/heartbeat"
  "github.com/nodestral/agent/pkg/provider"
  "github.com/nodestral/agent/pkg/register"
  "github.com/nodestral/agent/pkg/system"
  "github.com/nodestral/agent/pkg/terminal"
)

const version = "0.1.0"

func main() {
  configPath := flag.String("config", "", "path to agent config file")
  flag.Parse()

  log.Printf("Nodestral Agent v%s starting...", version)

  // Load config
  cfg, err := config.Load(*configPath)
  if err != nil {
    log.Fatalf("failed to load config: %v", err)
  }

  // Collect system info
  sysInfo, err := system.Collect()
  if err != nil {
    log.Fatalf("failed to collect system info: %v", err)
  }
  log.Printf("system: %s (%s) | %d vCPU | %d MB RAM | %d GB disk",
    sysInfo.Hostname, sysInfo.OS, sysInfo.CPUCores, sysInfo.RAMMB, sysInfo.DiskGB)

  // Detect cloud provider
  detector := provider.NewDetector()
  prov := detector.Detect(context.Background())
  log.Printf("provider: %s (%s)", prov.Name, prov.Region)

  // Register with API if not already registered
  if !cfg.IsRegistered() {
    log.Println("registering with Nodestral API...")
    if err := register.Register(cfg, sysInfo, prov); err != nil {
      log.Fatalf("registration failed: %v", err)
    }
    log.Printf("registered as node %s", cfg.NodeID)
  } else {
    log.Printf("already registered as node %s", cfg.NodeID)
  }

  // Setup graceful shutdown
  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()

  sigCh := make(chan os.Signal, 1)
  signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

  go func() {
    sig := <-sigCh
    log.Printf("received signal %v, shutting down...", sig)
    cancel()
  }()

  // Start heartbeat loop
 hb := heartbeat.New(cfg)
  go hb.Run(ctx)
  log.Println("heartbeat loop started")

  // Start discovery loop
  disc := discovery.New(cfg)
  go disc.StartLoop(ctx)
  log.Println("discovery loop started")

  // Start terminal client (reverse WebSocket to API)
  termClient := terminal.New(cfg)
  go termClient.StartLoop(ctx)
  log.Println("terminal client started")

  // Block until shutdown
  <-ctx.Done()
  log.Println("Nodestral Agent stopped")
}
