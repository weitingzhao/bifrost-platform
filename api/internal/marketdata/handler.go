package marketdata

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

type Handler struct {
	svc *Service
}

func NewHandler(clusterSvc *cluster.Service) *Handler {
	return &Handler{svc: NewService(clusterSvc)}
}

func (h *Handler) Service() *Service { return h.svc }

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Status(r.Context()))
}

// HandleWatchlistUnion returns the deduplicated union of watchlist symbols
// from all registered Trade environments (dev + stg + prod).
func (h *Handler) HandleWatchlistUnion(w http.ResponseWriter, r *http.Request) {
	resp := h.svc.WatchlistUnion(r.Context())
	writeJSON(w, http.StatusOK, resp)
}

// HandleAPIProxy proxies /api/v1/plugins/market-data/api/* → Plugin API (:8790)/*.
// Example: GET .../api/market/coverage/db-summary → GET {plugin}/market/coverage/db-summary
// POST/DELETE: Console sends PLATFORM_OPERATOR_TOKEN; this handler's Service.Proxy
// replaces it with MARKET_DATA_WRITE_TOKEN before the Plugin hop.
func (h *Handler) HandleAPIProxy(w http.ResponseWriter, r *http.Request) {
	suffix := chi.URLParam(r, "*")
	if suffix == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing plugin API path after /plugins/market-data/api/",
		})
		return
	}
	pluginPath := "/" + strings.TrimPrefix(suffix, "/")
	resp, err := h.svc.Proxy(r, pluginPath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "market-data plugin API unreachable: " + err.Error(),
			"hint":  "Ensure market-data-api is Running, or set MARKET_DATA_API_URL (e.g. http://127.0.0.1:8790)",
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "failed reading plugin API response: " + err.Error(),
		})
		return
	}

	// K8s API proxy returns Status objects (e.g. service not found) — normalize for Console.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500 {
		var kubeStatus struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
			Reason  string `json:"reason"`
		}
		if json.Unmarshal(body, &kubeStatus) == nil && kubeStatus.Kind == "Status" {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": kubeStatus.Message,
				"hint":  "Deploy market-data-api in plugin-market-data NS, or set MARKET_DATA_API_URL for local Plugin API",
			})
			return
		}
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
