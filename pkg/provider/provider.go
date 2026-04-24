package provider

import (
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "strings"
  "time"
)

// Provider represents a detected cloud provider.
type Provider struct {
  Name         string `json:"name"`
  Region       string `json:"region"`
  InstanceType string `json:"instance_type"`
}

// Detector attempts to detect the cloud provider from metadata services.
type Detector struct {
  httpClient *http.Client
}

// NewDetector creates a new cloud provider detector with short timeouts.
func NewDetector() *Detector {
  return &Detector{
    httpClient: &http.Client{Timeout: 2 * time.Second},
  }
}

// Detect attempts to identify the cloud provider by probing metadata services.
func (d *Detector) Detect(ctx context.Context) *Provider {
  // Try each provider's metadata endpoint with context timeout
  type probe struct {
    name   string
    detect func(ctx context.Context) (*Provider, error)
  }

  probes := []probe{
    {"tencent", d.detectTencent},
    {"aws", d.detectAWS},
    {"gcp", d.detectGCP},
    {"azure", d.detectAzure},
    {"hetzner", d.detectHetzner},
    {"digitalocean", d.detectDigitalOcean},
  }

  for _, p := range probes {
    probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
    prov, err := p.detect(probeCtx)
    cancel()
    if err == nil && prov != nil {
      return prov
    }
  }

  return &Provider{Name: "unknown", Region: ""}
}

func (d *Detector) detectTencent(ctx context.Context) (*Provider, error) {
  return d.detectMetadataWithInstanceType(ctx, "http://metadata.tencentyun.com/latest/meta-data/",
    map[string]string{
      "instance-id":  "instance-id",
      "placement/zone": "zone",
    },
    "instance/instance-type",
    "tencent")
}

func (d *Detector) detectAWS(ctx context.Context) (*Provider, error) {
  return d.detectMetadataWithInstanceType(ctx, "http://169.254.169.254/latest/meta-data/",
    map[string]string{
      "instance-id":              "instance-id",
      "placement/availability-zone": "zone",
    },
    "instance-type",
    "aws")
}

func (d *Detector) detectGCP(ctx context.Context) (*Provider, error) {
  req, err := http.NewRequestWithContext(ctx, "GET",
    "http://metadata.google.internal/computeMetadata/v1/instance/?recursive=true", nil)
  if err != nil {
    return nil, err
  }
  req.Header.Set("Metadata-Flavor", "Google")

  resp, err := d.httpClient.Do(req)
  if err != nil || resp.StatusCode != 200 {
    if resp != nil {
      resp.Body.Close()
    }
    return nil, fmt.Errorf("not GCP")
  }
  defer resp.Body.Close()

  var data struct {
    Zone string `json:"zone"`
  }
  if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
    return nil, err
  }

  // Zone format: projects/PROJECT/regions/REGION/zones/ZONE
  parts := strings.Split(data.Zone, "/")
  region := ""
  if len(parts) >= 4 {
    region = parts[3]
  }

  return &Provider{Name: "gcp", Region: region}, nil
}

func (d *Detector) detectAzure(ctx context.Context) (*Provider, error) {
  req, err := http.NewRequestWithContext(ctx, "GET",
    "http://169.254.169.254/metadata/instance?api-version=2021-02-01", nil)
  if err != nil {
    return nil, err
  }
  req.Header.Set("Metadata", "true")

  resp, err := d.httpClient.Do(req)
  if err != nil || resp.StatusCode != 200 {
    if resp != nil {
      resp.Body.Close()
    }
    return nil, fmt.Errorf("not Azure")
  }
  defer resp.Body.Close()

  var data struct {
    Compute struct {
      Location string `json:"location"`
    } `json:"compute"`
  }
  if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
    return nil, err
  }

  return &Provider{Name: "azure", Region: data.Compute.Location}, nil
}

func (d *Detector) detectHetzner(ctx context.Context) (*Provider, error) {
  resp, err := d.httpClient.Get("http://169.254.169.254/hetzner/v1/metadata")
  if err != nil || resp.StatusCode != 200 {
    if resp != nil {
      resp.Body.Close()
    }
    return nil, fmt.Errorf("not Hetzner")
  }
  defer resp.Body.Close()

  var data struct {
    AvailabilityZone string `json:"availability-zone"`
  }
  if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
    return nil, err
  }

  // Availability zone format: fsn1-dc14 → region is fsn1
  region := data.AvailabilityZone
  if idx := strings.Index(region, "-dc"); idx > 0 {
    region = region[:idx]
  }

  return &Provider{Name: "hetzner", Region: region}, nil
}

func (d *Detector) detectDigitalOcean(ctx context.Context) (*Provider, error) {
  resp, err := d.httpClient.Get("http://169.254.169.254/metadata/v1.json")
  if err != nil || resp.StatusCode != 200 {
    if resp != nil {
      resp.Body.Close()
    }
    return nil, fmt.Errorf("not DigitalOcean")
  }
  defer resp.Body.Close()

  var data struct {
    Region string `json:"region"`
  }
  if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
    return nil, err
  }

  return &Provider{Name: "digitalocean", Region: data.Region}, nil
}

// detectMetadataWithInstanceType is like detectMetadata but also fetches the instance type.
func (d *Detector) detectMetadataWithInstanceType(ctx context.Context, baseURL string, keys map[string]string, instanceTypeKey, providerName string) (*Provider, error) {
  var instanceID, zone, instanceType string

  // Fetch instance type
  req, err := http.NewRequestWithContext(ctx, "GET", baseURL+instanceTypeKey, nil)
  if err == nil {
    if resp, err := d.httpClient.Do(req); err == nil && resp.StatusCode == 200 {
      body, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
      instanceType = strings.TrimSpace(string(body))
      resp.Body.Close()
    } else if resp != nil {
      resp.Body.Close()
    }
  }

  for key := range keys {
    req, err := http.NewRequestWithContext(ctx, "GET", baseURL+key, nil)
    if err != nil {
      continue
    }
    resp, err := d.httpClient.Do(req)
    if err != nil || resp.StatusCode != 200 {
      if resp != nil {
        resp.Body.Close()
      }
      return nil, fmt.Errorf("not %s", providerName)
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
    val := strings.TrimSpace(string(body))

    if strings.Contains(key, "zone") {
      zone = val
    } else {
      instanceID = val
    }
  }

  if instanceID == "" {
    return nil, fmt.Errorf("not %s", providerName)
  }

  return &Provider{Name: providerName, Region: zone, InstanceType: instanceType}, nil
}

// detectMetadata is a generic helper for simple key-based metadata endpoints.
func (d *Detector) detectMetadata(ctx context.Context, baseURL string, keys map[string]string, providerName string) (*Provider, error) {
  var instanceID, zone string

  for key := range keys {
    req, err := http.NewRequestWithContext(ctx, "GET", baseURL+key, nil)
    if err != nil {
      continue
    }
    resp, err := d.httpClient.Do(req)
    if err != nil || resp.StatusCode != 200 {
      if resp != nil {
        resp.Body.Close()
      }
      return nil, fmt.Errorf("not %s", providerName)
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
    val := strings.TrimSpace(string(body))

    if key == "instance-id" || key == "placement/zone" {
      if strings.Contains(key, "zone") {
        zone = val
      } else {
        instanceID = val
      }
    }
    if strings.Contains(key, "zone") {
      zone = val
    }
  }

  if instanceID == "" {
    return nil, fmt.Errorf("not %s", providerName)
  }

  return &Provider{Name: providerName, Region: zone}, nil
}
