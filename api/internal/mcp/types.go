package mcp

import "time"

type ToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Level       string `json:"level"`
	Method      string `json:"method,omitempty"`
	Route       string `json:"route,omitempty"`
	Role        string `json:"role,omitempty"`
	Phase       string `json:"phase,omitempty"`
	Implemented bool   `json:"implemented"`
}

type ToolsResponse struct {
	ServerName       string     `json:"server_name"`
	ServerVersion    string     `json:"server_version"`
	ContractVersion  string     `json:"contract_version"`
	Tools            []ToolView `json:"tools"`
	ImplementedCount int        `json:"implemented_count"`
	GeneratedAt      time.Time  `json:"generated_at"`
}

type StatusResponse struct {
	ServerName      string            `json:"server_name"`
	ServerVersion   string            `json:"server_version"`
	Transport       string            `json:"transport"`
	PlatformAPIURL  string            `json:"platform_api_url"`
	ScriptPath      string            `json:"script_path"`
	CursorConfig    CursorConfigHints `json:"cursor_config"`
	ToolCount       int               `json:"tool_count"`
	ImplementedCount int              `json:"implemented_count"`
	GeneratedAt     time.Time         `json:"generated_at"`
}

type CursorConfigHints struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []string `json:"env"`
}
