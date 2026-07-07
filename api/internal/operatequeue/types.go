package operatequeue

const stateVersion = "2026-07-07"

const (
	StatusOpen   = "open"
	StatusClosed = "closed"
)

const (
	SourcePostCompletion = "post_completion"
	SourceManual         = "manual"
)

// ValidLanes are optional operate lanes (D11).
var ValidLanes = map[string]bool{
	"governance":         true,
	"troubleshoot":       true,
	"release":            true,
	"business-advisory":  true,
}

type Item struct {
	ID          string `json:"id"`
	ProgramID   string `json:"program_id"`
	Lane        string `json:"lane,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	ClosedAt    string `json:"closed_at,omitempty"`
	Source      string `json:"source,omitempty"`
	PendingID   string `json:"pending_id,omitempty"`
	ApprovedBy  string `json:"approved_by,omitempty"`
}

type FileRecord struct {
	Version string `json:"version"`
	Items   []Item `json:"items"`
}

type ListResponse struct {
	Open          []Item `json:"open"`
	RecentClosed  []Item `json:"recent_closed"`
}

type EnqueueRequest struct {
	ProgramID   string `json:"program_id"`
	Lane        string `json:"lane,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type ApprovalInjectParams struct {
	PendingID   string
	ProgramID   string
	Title       string
	Description string
	Lane        string
	ApprovedBy  string
}
