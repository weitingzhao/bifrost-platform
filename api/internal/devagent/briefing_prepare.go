package devagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	briefingPackRelPath = "data/briefing/active-pack.md"
	briefingMetaRelPath = "data/briefing/active-meta.json"
)

type BriefingPrepareRequest struct {
	SessionPack string `json:"session_pack"`
	SessionID   string `json:"session_id,omitempty"`
	ProgramID   string `json:"program_id,omitempty"`
	PhaseID     string `json:"phase_id,omitempty"`
	Lane        string `json:"lane,omitempty"`
	Intent      string `json:"intent,omitempty"`
}

type BriefingPrepareResponse struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	MetaPath string `json:"meta_path,omitempty"`
	Message string `json:"message,omitempty"`
}

type BriefingActiveMeta struct {
	SessionID string `json:"session_id,omitempty"`
	ProgramID string `json:"program_id,omitempty"`
	PhaseID   string `json:"phase_id,omitempty"`
	Lane      string `json:"lane,omitempty"`
	Intent    string `json:"intent,omitempty"`
	PreparedAt string `json:"prepared_at"`
}

func (h *Handler) briefingDir() string {
	return filepath.Join(h.repoRoot, "data", "briefing")
}

func (h *Handler) briefingPackPath() string {
	return filepath.Join(h.briefingDir(), "active-pack.md")
}

func (h *Handler) briefingMetaPath() string {
	return filepath.Join(h.briefingDir(), "active-meta.json")
}

func atomicWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// HandleBriefingPrepare writes the session pack to data/briefing/active-pack.md
// for Cursor IDE /briefing command consumption. Requires operator auth.
func (h *Handler) HandleBriefingPrepare(w http.ResponseWriter, r *http.Request) {
	var req BriefingPrepareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	pack := strings.TrimSpace(req.SessionPack)
	if pack == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_pack required"})
		return
	}

	packPath := h.briefingPackPath()
	if err := atomicWriteFile(packPath, []byte(pack+"\n")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "write pack: " + err.Error(),
		})
		return
	}

	meta := BriefingActiveMeta{
		SessionID:  strings.TrimSpace(req.SessionID),
		ProgramID:  strings.TrimSpace(req.ProgramID),
		PhaseID:    strings.TrimSpace(req.PhaseID),
		Lane:       strings.TrimSpace(req.Lane),
		Intent:     strings.TrimSpace(req.Intent),
		PreparedAt: time.Now().UTC().Format(time.RFC3339),
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "marshal meta"})
		return
	}
	metaPath := h.briefingMetaPath()
	if err := atomicWriteFile(metaPath, metaBytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "write meta: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, BriefingPrepareResponse{
		Status:   "ready",
		Path:     briefingPackRelPath,
		MetaPath: briefingMetaRelPath,
	})
}
