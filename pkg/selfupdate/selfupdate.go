package selfupdate

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	githubOwner = "nodestral"
	githubRepo  = "agent"
)

// CheckResponse is what the heartbeat returns.
type CheckResponse struct {
	LatestAgentVersion string `json:"latest_agent_version"`
}

// GitHubRelease is a minimal GitHub release struct.
type GitHubRelease struct {
	TagName string  `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Check compares current version with the latest from heartbeat response.
// If an update is available, it downloads and performs a self-restart.
func Check(currentVersion string, heartbeatResp map[string]interface{}) {
	latest, _ := heartbeatResp["latest_agent_version"].(string)
	if latest == "" {
		return
	}

	if !newer(latest, currentVersion) {
		return
	}

	log.Printf("selfupdate: new version available: %s → %s", currentVersion, latest)

	if err := update(latest); err != nil {
		log.Printf("selfupdate: failed: %v", err)
		return
	}

	log.Printf("selfupdate: restarting with version %s", latest)
	restartSelf()
}

// newer returns true if a > b using semver comparison.
func newer(a, b string) bool {
	re := regexp.MustCompile(`(\d+)`)
	aParts := re.FindAllString(strings.TrimPrefix(a, "v"), -1)
	bParts := re.FindAllString(strings.TrimPrefix(b, "v"), -1)

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		av, bv := 0, 0
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &av)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bv)
		}
		if av > bv {
			return true
		}
		if av < bv {
			return false
		}
	}
	return false
}

// update downloads the new binary and replaces the current one.
func update(version string) error {
	// Detect OS/arch
	goos := "linux"
	goarch := "amd64"
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		arch := strings.TrimSpace(string(out))
		if arch == "aarch64" || arch == "arm64" {
			goarch = "arm64"
		}
	}
	if out, err := exec.Command("uname", "-s").Output(); err == nil {
		if strings.TrimSpace(string(out)) == "Darwin" {
			goos = "darwin"
		}
	}

	assetName := fmt.Sprintf("nodestral-agent-%s_%s", goos, goarch)
	downloadURL := fmt.Sprintf(
		"https://github.com/%s/%s/releases/download/%s/%s",
		githubOwner, githubRepo, version, assetName,
	)

	log.Printf("selfupdate: downloading %s", downloadURL)

	// Download to temp file
	tmpFile, err := os.CreateTemp("", "nodestral-agent-update-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	tmpFile.Close()

	// Make it executable
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Get current binary path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get exe path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	// Rename old binary
	backupPath := exePath + ".old"
	os.Remove(backupPath)
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("rename old: %w", err)
	}

	// Copy new binary to the location
	newBin, err := os.Open(tmpFile.Name())
	if err != nil {
		// Rollback
		os.Rename(backupPath, exePath)
		return fmt.Errorf("open new: %w", err)
	}
	defer newBin.Close()

	dst, err := os.OpenFile(exePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		os.Rename(backupPath, exePath)
		return fmt.Errorf("open dst: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, newBin); err != nil {
		os.Rename(backupPath, exePath)
		return fmt.Errorf("copy: %w", err)
	}
	dst.Close()

	// Clean up backup
	os.Remove(backupPath)

	log.Printf("selfupdate: binary replaced at %s", exePath)
	return nil
}

// restartSelf replaces the current process with the new binary.
func restartSelf() {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("selfupdate: cannot find exe: %v", err)
		return
	}

	args := os.Args
	env := os.Environ()

	execErr := syscall.Exec(exePath, args, env)
	if execErr != nil {
		log.Printf("selfupdate: exec failed: %v", execErr)
	}
}
