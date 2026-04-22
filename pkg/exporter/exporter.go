package exporter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
	"github.com/nodestral/agent/pkg/config"
	"github.com/nodestral/agent/pkg/system"
)

// BackendConfig holds the remote write endpoint configuration.
type BackendConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Exporter pushes metrics to a Prometheus remote write endpoint.
type Exporter struct {
	client   *http.Client
	config   *BackendConfig
	configMu sync.RWMutex
	hostname string
	nodeID   string
	failCnt  int
}

// New creates a new exporter.
func New(hostname, nodeID string) *Exporter {
	return &Exporter{
		client:   &http.Client{Timeout: 10 * time.Second},
		hostname: hostname,
		nodeID:   nodeID,
	}
}

// UpdateConfig changes the backend configuration. Pass nil to disable.
func (e *Exporter) UpdateConfig(cfg *BackendConfig) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.config = cfg
	e.failCnt = 0
	if cfg != nil {
		log.Printf("exporter: configured → %s", cfg.URL)
	} else {
		log.Println("exporter: disabled")
	}
}

// Push sends metrics to the configured backend.
func (e *Exporter) Push(ctx context.Context, m *system.Metrics) {
	e.configMu.RLock()
	cfg := e.config
	e.configMu.RUnlock()

	if cfg == nil || cfg.URL == "" {
		return
	}

	// Encode nodestral metrics
	data, err := e.encode(m)
	if err != nil {
		log.Printf("exporter: encode: %v", err)
		return
	}

	// Scrape node_exporter if available and append
	if neData := e.scrapeNodeExporter(); neData != nil {
		data = append(data, neData...)
	}

	compressed := snappy.Encode(nil, data)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.URL, bytes.NewReader(compressed))
	if err != nil {
		log.Printf("exporter: request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		e.failCnt++
		if e.failCnt <= 3 {
			log.Printf("exporter: push failed (%d): %v", e.failCnt, err)
		}
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		e.failCnt++
		if e.failCnt <= 5 {
			log.Printf("exporter: server error (status %d): %s", resp.StatusCode, string(body))
		}
		return
	}
	if e.failCnt > 0 {
		log.Printf("exporter: reconnected after %d failures", e.failCnt)
		e.failCnt = 0
	}
}

func (e *Exporter) encode(m *system.Metrics) ([]byte, error) {
	ts := time.Now().UnixMilli()

	metrics := []struct {
		name  string
		value float64
	}{
		{"nodestral_cpu_percent", m.CPUPercent},
		{"nodestral_ram_percent", m.RAMPercent},
		{"nodestral_ram_used_bytes", float64(m.RAMUsedMB * 1024 * 1024)},
		{"nodestral_disk_percent", m.DiskPercent},
		{"nodestral_disk_used_bytes", float64(m.DiskUsedGB * 1024 * 1024 * 1024)},
		{"nodestral_net_rx_bytes", float64(m.NetRxBytes)},
		{"nodestral_net_tx_bytes", float64(m.NetTxBytes)},
		{"nodestral_load_1m", m.Load1m},
		{"nodestral_load_5m", m.Load5m},
	}

	series := make([]prompb.TimeSeries, 0, len(metrics))
	for _, metric := range metrics {
		series = append(series, prompb.TimeSeries{
			Labels: []prompb.Label{
				{Name: "__name__", Value: metric.name},
				{Name: "node", Value: e.hostname},
				{Name: "node_id", Value: e.nodeID},
			},
			Samples: []prompb.Sample{
				{Value: metric.value, Timestamp: ts},
			},
		})
	}

	req := &prompb.WriteRequest{Timeseries: series}
	return proto.Marshal(req)
}

// Fetcher periodically pulls backend config from the API.
type Fetcher struct {
	cfg      *config.Config
	exp      *Exporter
	interval time.Duration
	client   *http.Client
}

// NewFetcher creates a config fetcher that updates the exporter.
func NewFetcher(cfg *config.Config, exp *Exporter) *Fetcher {
	return &Fetcher{
		cfg:      cfg,
		exp:      exp,
		interval: 5 * time.Minute,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Run polls the API for backend config, blocking until context is cancelled.
func (f *Fetcher) Run(ctx context.Context) {
	f.fetch(ctx)

	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.fetch(ctx)
		}
	}
}

func (f *Fetcher) fetch(ctx context.Context) {
	if f.cfg.NodeID == "" || f.cfg.AuthToken == "" {
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		f.cfg.APIURL+"/agent/exporter-config", nil)
	if err != nil {
		log.Printf("exporter: fetch config request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+f.cfg.AuthToken)

	resp, err := f.client.Do(req)
	if err != nil {
		log.Printf("exporter: fetch config: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		f.exp.UpdateConfig(nil)
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("exporter: fetch config status %d", resp.StatusCode)
		return
	}

	var result struct {
		Endpoint string `json:"endpoint"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("exporter: decode config: %v", err)
		return
	}

	if result.Endpoint == "" {
		f.exp.UpdateConfig(nil)
		return
	}

	f.exp.UpdateConfig(&BackendConfig{
		URL:      result.Endpoint,
		Username: result.Username,
		Password: result.Password,
	})
}

var _ = fmt.Sprint

// scrapeNodeExporter scrapes localhost:9100/metrics and returns prompb encoded bytes.
func (e *Exporter) scrapeNodeExporter() []byte {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://localhost:9100/metrics")
	if err != nil {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil
	}
	defer resp.Body.Close()

	// Parse Prometheus exposition format and encode to prompb
	ts := time.Now().UnixMilli()
	series := e.parsePromText(resp.Body, ts)
	if len(series) == 0 {
		return nil
	}

	req := &prompb.WriteRequest{Timeseries: series}
	data, err := proto.Marshal(req)
	if err != nil {
		log.Printf("exporter: node_exporter encode: %v", err)
		return nil
	}
	return data
}

// parsePromText parses Prometheus text format into prompb TimeSeries.
func (e *Exporter) parsePromText(r io.Reader, ts int64) []prompb.TimeSeries {
	var series []prompb.TimeSeries
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	baseLabels := []prompb.Label{
		{Name: "node", Value: e.hostname},
		{Name: "node_id", Value: e.nodeID},
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse: metric_name{labels} value [timestamp]
		// Split on first space to separate metric from value
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}

		metricPart := strings.TrimSpace(parts[0])
		valueStr := strings.TrimSpace(parts[1])

		// Skip histogram buckets and summaries (too many series)
		if strings.Contains(metricPart, "_bucket") || strings.Contains(metricPart, "_sum") || strings.Contains(metricPart, "_count") {
			if strings.Contains(metricPart, "scrape") {
				// Keep scrape metrics
			} else {
				continue
			}
		}

		// Skip HELP/TYPE lines that might slip through
		if strings.HasPrefix(valueStr, "#") {
			continue
		}

		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}

		// Skip NaN, Inf, -Inf
		if value != value || value > 1e300 || value < -1e300 {
			continue
		}

		// Parse metric name and labels
		var metricName string
		var extraLabels []prompb.Label

		bracketIdx := strings.Index(metricPart, "{")
		if bracketIdx >= 0 {
			metricName = metricPart[:bracketIdx]
			labelStr := metricPart[bracketIdx+1 : len(metricPart)-1]
			if labelStr != "" {
				for _, pair := range strings.Split(labelStr, ",") {
					eqIdx := strings.Index(pair, "=")
					if eqIdx < 0 {
						continue
					}
					key := strings.TrimSpace(pair[:eqIdx])
					val := strings.TrimSpace(pair[eqIdx+1:])
					val = strings.Trim(val, "\"")
					extraLabels = append(extraLabels, prompb.Label{Name: key, Value: val})
				}
			}
		} else {
			metricName = metricPart
		}

		// Build label set: __name__ + base labels + extra labels
		labels := make([]prompb.Label, 0, 2+len(baseLabels)+len(extraLabels))
		labels = append(labels, prompb.Label{Name: "__name__", Value: metricName})
		labels = append(labels, baseLabels...)
		labels = append(labels, extraLabels...)

		series = append(series, prompb.TimeSeries{
			Labels:  labels,
			Samples: []prompb.Sample{{Value: value, Timestamp: ts}},
		})
	}

	return series
}
