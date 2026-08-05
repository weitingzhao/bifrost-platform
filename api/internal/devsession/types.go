package devsession

// DevSession represents a single managed service (bdev pane or K8s Deployment).
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
	LastOutputAt *int64 `json:"last_output_at,omitempty"`
	// Crashed is true when bdev-supervise hit restart_max_failures (may be cooling).
	Crashed *bool `json:"crashed,omitempty"`
	// LastExitCode is the child exit code from the last supervised run.
	LastExitCode *int `json:"last_exit_code,omitempty"`
	// CoolingUntil is unix seconds when supervise will clear crash and retry.
	CoolingUntil *int64 `json:"cooling_until,omitempty"`

	// Mode is "bdev" (local) or "k8s" (cluster). Omitted by older clients is fine.
	Mode string `json:"mode,omitempty"`
	// Env is the viewer seat that produced this session (dev / stg / prod).
	Env string `json:"env,omitempty"`
	// Namespace is set in K8s mode.
	Namespace string `json:"namespace,omitempty"`
	// ImageTag is the first container image tag (K8s mode).
	ImageTag string `json:"image_tag,omitempty"`
	// ReadyReplicas / DesiredReplicas are set in K8s mode.
	ReadyReplicas   *int32 `json:"ready_replicas,omitempty"`
	DesiredReplicas *int32 `json:"desired_replicas,omitempty"`
}

// ControlRequest is the body for POST /{name}/control.
type ControlRequest struct {
	Action string `json:"action"` // "start" | "stop" | "restart" | "clear-logs"
}

// ControlResponse is returned after a control action.
type ControlResponse struct {
	Name    string `json:"name"`
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// LogResponse returns tail lines from a service log file or pod logs.
type LogResponse struct {
	Name  string   `json:"name"`
	Lines []string `json:"lines"`
}
