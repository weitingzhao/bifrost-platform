package sessions

// Record is a Session Job archive under data/sessions/{session_id}.json.
type Record struct {
	SessionID     string `json:"session_id"`
	ProgramID     string `json:"program_id"`
	PhaseID       string `json:"phase_id"`
	LaneID        string `json:"lane_id,omitempty"`
	PackHash      string `json:"pack_hash,omitempty"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	CursorAgentID string `json:"cursor_agent_id,omitempty"`
}

type CreateRequest struct {
	SessionID     string `json:"session_id,omitempty"`
	ProgramID     string `json:"program_id"`
	PhaseID       string `json:"phase_id"`
	LaneID        string `json:"lane_id,omitempty"`
	PackHash      string `json:"pack_hash,omitempty"`
	Pack          string `json:"pack,omitempty"` // optional raw pack — hashed if pack_hash empty
	Status        string `json:"status,omitempty"`
	CursorAgentID string `json:"cursor_agent_id,omitempty"`
}

type ListResponse struct {
	Sessions []Record `json:"sessions"`
}

const (
	StatusOpen     = "open"
	StatusClosed   = "closed"
	StatusProgress = "in_progress"
)
