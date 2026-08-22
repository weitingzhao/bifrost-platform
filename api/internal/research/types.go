package research

import "time"

// StatusResponse is a minimal health probe for Research API (:8795).
type StatusResponse struct {
	Reachable   bool      `json:"reachable"`
	Error       string    `json:"error,omitempty"`
	Hint        string    `json:"hint,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
}
