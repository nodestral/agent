package nodeexporter

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	binaryPath  = "/opt/nodestral/node_exporter/node_exporter"
	servicePath = "/etc/systemd/system/node_exporter.service"
	port        = 9100
	version     = "1.8.0"
)

// Status returns the current state of node_exporter.
type Status struct {
	Installed  bool   `json:"installed"`
	Running    bool   `json:"running"`
	Version    string `json:"version,omitempty"`
	ScrapeURL  string `json:"scrape_url,omitempty"`
	RunningPID int    `json:"pid,omitempty"`
}

// GetStatus checks if node_exporter is installed and running.
func GetStatus() *Status {
	s := &Status{}

	if _, err := os.Stat(binaryPath); err == nil {
		s.Installed = true
		s.Version = version
		s.ScrapeURL = fmt.Sprintf("http://localhost:%d/metrics", port)

		// Check if running via systemctl
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "systemctl", "is-active", "node_exporter")
		out, _ := cmd.CombinedOutput()
		if strings.TrimSpace(string(out)) == "active" {
			s.Running = true
		}

		// Get PID
		cmd2 := exec.CommandContext(ctx, "systemctl", "show", "node_exporter", "--property=MainPID")
		out2, _ := cmd2.CombinedOutput()
		if pidStr := strings.TrimPrefix(strings.TrimSpace(string(out2)), "MainPID="); pidStr != "" && pidStr != "0" {
			fmt.Sscanf(pidStr, "%d", &s.RunningPID)
		}
	}

	return s
}

// Install downloads and sets up node_exporter.
func Install() error {
	if _, err := os.Stat(binaryPath); err == nil {
		log.Printf("node_exporter: already installed at %s, starting...", binaryPath)
		return startNodeExporter()
	}

	// Determine arch
	goArch := runtime.GOARCH
	arch := goArch
	if goArch == "amd64" {
		arch = "amd64"
	} else if goArch == "arm64" {
		arch = "arm64"
	} else {
		return fmt.Errorf("unsupported architecture: %s", goArch)
	}

	url := fmt.Sprintf(
		"https://github.com/prometheus/node_exporter/releases/download/v%s/node_exporter-%s.linux-%s.tar.gz",
		version, version, arch,
	)

	log.Printf("node_exporter: downloading %s", url)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Extract binary from tar.gz
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		// Look for the node_exporter binary
		if strings.HasSuffix(hdr.Name, "node_exporter") && !strings.Contains(filepath.Base(hdr.Name), ".") {
			// Write to temp file then move
			tmpPath := binaryPath + ".tmp"
			f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return fmt.Errorf("write binary: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				os.Remove(tmpPath)
				return fmt.Errorf("copy binary: %w", err)
			}
			f.Close()

			if err := os.Rename(tmpPath, binaryPath); err != nil {
				// May need elevated permissions
				return fmt.Errorf("install binary (may need root): %w", err)
			}
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("node_exporter binary not found in archive")
	}

	log.Printf("node_exporter: installed to %s", binaryPath)
	return startNodeExporter()
	return nil
}

// Uninstall stops and removes node_exporter.
func Uninstall() error {
	runCmd("systemctl", "stop", "node_exporter")
	runCmd("systemctl", "disable", "node_exporter")
	os.Remove(servicePath)
	os.Remove(binaryPath)
	runCmd("systemctl", "daemon-reload")
	log.Println("node_exporter: uninstalled")
	return nil
}

func startNodeExporter() error {
	started := false
	if err := runCmd("systemctl", "start", "node_exporter"); err != nil {
		log.Printf("node_exporter: systemd start failed, starting directly: %v", err)
		cmd := exec.Command(binaryPath, fmt.Sprintf("--web.listen-address=:%d", port), "--no-collector.ntp")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start process: %w", err)
		}
		started = true
	} else {
		started = true
	}
	if started {
		log.Println("node_exporter: started on port", port)
	}
	return nil
}

func runCmd(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s: %w", name, string(out), err)
	}
	return nil
}
