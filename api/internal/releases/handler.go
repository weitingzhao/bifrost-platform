package releases

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler serves GET /api/v1/releases.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Service() *Service { return h.svc }

type listResponse struct {
	Status  Status   `json:"status"`
	Records []Record `json:"records"`
	Error   string   `json:"error,omitempty"`
}

// HandleList returns release records, newest first. ?lane= ?env= filter; ?limit= (default 100).
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	resp := listResponse{Status: h.svc.Status(), Records: []Record{}}
	recs, err := h.svc.List(r.Context())
	if err != nil {
		resp.Error = err.Error()
	}
	for _, rec := range recs {
		if (q.Get("lane") != "" && rec.Lane != q.Get("lane")) || (q.Get("env") != "" && rec.Env != q.Get("env")) {
			continue
		}
		if len(resp.Records) >= limit {
			break
		}
		resp.Records = append(resp.Records, rec)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
