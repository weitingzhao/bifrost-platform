package tradeagent

import (
	"encoding/json"
	"net/http"
)

type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) HandleDomains(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"domains":      Domains(),
		"domain_count": len(Domains()),
		"mode":         "read_only",
	})
}

func (h *Handler) HandleCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, CatalogResponseNow())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
