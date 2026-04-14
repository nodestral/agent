package terminal

// Terminal is a placeholder for the SSH bridge feature.
// In future iterations, this will proxy WebSocket connections
// from the API server to a local PTY for web-based terminal access.
//
// Design:
//   - Agent listens for WebSocket upgrade requests from API server
//   - Spawns a PTY session (e.g., /bin/bash)
//   - Bi-directional data flow: WebSocket ↔ PTY
//   - Session recording for audit logs (optional)
type Terminal struct {
  // TODO: implement WebSocket SSH bridge
}
