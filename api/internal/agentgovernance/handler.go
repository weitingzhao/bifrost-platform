package agentgovernance

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store     *remediation.JobStore
	overrides TrustOverrideStore
}

func NewHandler(store *remediation.JobStore) *Handler {
	_ = ensureAgentTasks()
	return &Handler{store: store, overrides: NewYAMLTrustOverrideStore("config/trust-overrides.yaml")}
}

// UseTrustOverrideStore points this handler at the release file.
func (h *Handler) UseTrustOverrideStore(s TrustOverrideStore) { h.overrides = s }

// TrustOverrideLocation names where this instance keeps the overrides.
func (h *Handler) TrustOverrideLocation() string { return h.overrides.Location() }

// listOverrides returns an empty map and a store_error string when the file
// cannot be read. Callers still answer 200: Research reads the matrix daily.
func (h *Handler) listOverrides(r *http.Request) (map[string]TrustOverride, string) {
	o, err := h.overrides.List(r.Context())
	if err != nil {
		slog.Error("trust overrides unreadable", "store", h.overrides.Location(), "err", err)
		return map[string]TrustOverride{}, err.Error()
	}
	if o == nil {
		o = map[string]TrustOverride{}
	}
	return o, ""
}

func (h *Handler) HandleTrustMatrix(w http.ResponseWriter, r *http.Request) {
	overrides, storeErr := h.listOverrides(r)
	jobs := h.store.List()
	raw := computeTrustMatrixRaw(jobs)
	resp := ApplyTrustOverrides(raw, overrides)
	resp.OverrideStore = h.overrides.Location()
	resp.StoreError = storeErr
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleTrustOverrides(w http.ResponseWriter, r *http.Request) {
	overrides, storeErr := h.listOverrides(r)
	writeJSON(w, http.StatusOK, TrustOverridesResponse{
		GeneratedAt: time.Now().UTC(),
		Overrides:   overrides,
		Store:       h.overrides.Location(),
		StoreError:  storeErr,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
