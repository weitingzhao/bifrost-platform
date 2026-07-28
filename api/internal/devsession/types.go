package devsession

// DevSession represents a single bdev-managed service.
type DevSession struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Group       string `json:"group"`
	Ports       []int  `json:"ports,omitempty"`
	Status      string `json:"status"`
	PID         int    `json:"pid,omitempty"`
	UptimeSec   int    `json:"uptime_sec,omitempty"`
	HealthOK    *bool  `json:"health_ok,omitempty"`
	Restarts    int    `json:"restarts,omitempty"`
	LogBytes    int64  `json:"log_bytes,omitempty"`
	LogMaxBytes int64  `json:"log_max_bytes,omitempty"`
}

// ControlRequest is the body for POST /{name}/control.
type ControlRequest struct {
	Action string `json:"action"` // "start" | "stop" | "restart"
}

// ControlResponse is returned after a control action.
type ControlResponse struct {
	Name    string `json:"name"`
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// LogResponse returns tail lines from a service log file.
type LogResponse struct {
	Name  string   `json:"name"`
	Lines []string `json:"lines"`
}
