package mcp

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) HandleTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ToolsResponseNow())
}

func (h *Handler) HandleStatus(w http.ResponseWriter, _ *http.Request) {
	tools := Catalog()
	impl := 0
	for _, t := range tools {
		if t.Implemented {
			impl++
		}
	}
	apiURL := os.Getenv("PLATFORM_API_URL")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:8780"
	}
	scriptPath := resolveMcpScriptPath()
	writeJSON(w, http.StatusOK, StatusResponse{
		ServerName:       ServerName,
		ServerVersion:    ServerVersion,
		Transport:        "stdio",
		PlatformAPIURL:   apiURL,
		ScriptPath:       scriptPath,
		CursorConfig:     cursorConfigHints(scriptPath, apiURL),
		ToolCount:        len(tools),
		ImplementedCount: impl,
		GeneratedAt:      time.Now().UTC(),
	})
}

func cursorConfigHints(scriptPath, apiURL string) CursorConfigHints {
	return CursorConfigHints{
		Command: "npx",
		Args:    []string{"tsx", scriptPath},
		Env: []string{
			"PLATFORM_API_URL=" + apiURL,
			"PLATFORM_OPERATOR_TOKEN=<operator-or-admin-token>",
		},
	}
}

func resolveMcpScriptPath() string {
	const rel = "mcp/platform/src/index.ts"
	if wd, err := os.Getwd(); err == nil {
		for _, base := range []string{wd, filepath.Join(wd, ".."), filepath.Join(wd, "../..")} {
			p := filepath.Join(base, rel)
			if _, err := os.Stat(p); err == nil {
				if abs, err := filepath.Abs(p); err == nil {
					return abs
				}
				return p
			}
		}
	}
	if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
		p := filepath.Join(root, rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return rel
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
