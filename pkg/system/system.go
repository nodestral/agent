package system

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

// Info contains basic system information about the node.
type Info struct {
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Kernel    string `json:"kernel"`
	Arch      string `json:"arch"`
	CPUCores  int    `json:"cpu_cores"`
	RAMMB     uint64 `json:"ram_mb"`
	DiskGB    uint64 `json:"disk_gb"`
	PublicIP  string `json:"public_ip,omitempty"`
	PrivateIP string `json:"private_ip,omitempty"`
}

// Collect gathers system information from the host.
func Collect() (*Info, error) {
	info := &Info{}

	// Hostname
	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	// OS info
	if hinfo, err := host.Info(); err == nil {
		info.OS = fmt.Sprintf("%s %s", hinfo.Platform, hinfo.PlatformVersion)
		info.Kernel = hinfo.KernelVersion
		info.Arch = runtime.GOARCH
	}

	// CPU cores
	if count, err := cpu.Counts(true); err == nil {
		info.CPUCores = count
	}

	// RAM
	if vmem, err := mem.VirtualMemory(); err == nil {
		info.RAMMB = vmem.Total / 1024 / 1024
	}

	// Disk (root filesystem)
	if dusage, err := disk.Usage("/"); err == nil {
		info.DiskGB = dusage.Total / 1024 / 1024 / 1024
	}

	// IPs
	info.PublicIP, info.PrivateIP = detectIPs()

	return info, nil
}

// detectIPs attempts to determine public and private IP addresses.
func detectIPs() (publicIP, privateIP string) {
	// Get all network interface addresses
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}
			ip := ipNet.IP.String()
			if isPrivateIP(ipNet.IP) {
				if privateIP == "" {
					privateIP = ip
				}
			} else {
				if publicIP == "" {
					publicIP = ip
				}
			}
		}
	}

	// Fall back to metadata-based public IP detection
	if publicIP == "" {
		publicIP = detectPublicIPFromMetadata()
	}

	return publicIP, privateIP
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// detectPublicIPFromMetadata tries common metadata endpoints and external IP services.
func detectPublicIPFromMetadata() string {
	// Try metadata services first (fast, no external dependency)
	metadataURLs := []string{
		"http://metadata.tencentyun.com/latest/meta-data/public-ipv4",
		"http://169.254.169.254/latest/meta-data/public-ipv4", // AWS
		"http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip",
	}
	for _, u := range metadataURLs {
		client := &http.Client{Timeout: 2 * time.Second}
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			continue
		}
		if strings.Contains(u, "google") {
			req.Header.Set("Metadata-Flavor", "Google")
		}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	// Fallback: external IP services
	services := []string{
		"https://api.ipify.org",
		"https://ifconfig.me",
	}
	client := &http.Client{Timeout: 3 * time.Second}
	for _, s := range services {
		resp, err := client.Get(s)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}
