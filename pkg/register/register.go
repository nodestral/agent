package register

import (
  "bytes"
  "encoding/json"
  "fmt"
  "io"
  "net/http"

  "github.com/nodestral/agent/pkg/config"
  "github.com/nodestral/agent/pkg/provider"
  "github.com/nodestral/agent/pkg/system"
)

// Request is the payload sent to the API on first registration.
type Request struct {
  System   *system.Info   `json:"system"`
  Provider *provider.Provider `json:"provider"`
}

// Response is the API response after successful registration.
type Response struct {
  NodeID    string `json:"node_id"`
  AuthToken string `json:"auth_token"`
}

// Register performs first-time registration with the Nodestral API.
// It sends system info and receives a node_id + auth_token, which are saved to config.
func Register(cfg *config.Config, sysInfo *system.Info, prov *provider.Provider) error {
  if cfg.IsRegistered() {
    return nil
  }

  reqBody := Request{
    System:   sysInfo,
    Provider: prov,
  }

  data, err := json.Marshal(reqBody)
  if err != nil {
    return fmt.Errorf("marshal register request: %w", err)
  }

  resp, err := http.Post(cfg.APIURL+"/agent/register", "application/json", bytes.NewReader(data))
  if err != nil {
    return fmt.Errorf("register request failed: %w", err)
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
    body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
    return fmt.Errorf("register failed (status %d): %s", resp.StatusCode, string(body))
  }

  var regResp Response
  if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
    return fmt.Errorf("decode register response: %w", err)
  }

  cfg.NodeID = regResp.NodeID
  cfg.AuthToken = regResp.AuthToken

  if err := cfg.Save(""); err != nil {
    return fmt.Errorf("save config after registration: %w", err)
  }

  return nil
}
