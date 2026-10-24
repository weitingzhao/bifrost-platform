package network

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

func (h *Handler) HandleClientByMAC(w http.ResponseWriter, r *http.Request) {
	record, err := h.svc.ClientByMAC(r.Context(), chi.URLParam(r, "mac"))
	if err != nil {
		h.writeFixedIPError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) HandleClientFixedIP(w http.ResponseWriter, r *http.Request) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	err := decoder.Decode(&fields)
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = errors.New("expected a single JSON object")
		}
	}
	// Decode exact catalog keys. encoding/json's case-insensitive struct fields
	// would disagree with the approval guard's case-sensitive parameter map.
	var req FixedIPRequest
	if err == nil {
		if len(fields) != 3 {
			err = errors.New("expected mac, ip and name")
		}
		for key, target := range map[string]*string{"mac": &req.MAC, "ip": &req.IP, "name": &req.Name} {
			raw, ok := fields[key]
			if !ok || json.Unmarshal(raw, target) != nil || *target == "" {
				err = errors.New("missing or invalid " + key)
			}
		}
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fixed IP JSON body: " + err.Error()})
		return
	}
	result, err := h.svc.SetClientFixedIP(r.Context(), req)
	status, detail := "ok", "fixed IP and name verified"
	if err != nil {
		status, detail = "failed", err.Error()
	} else if result["no_op"] == true {
		detail = "no-op: fixed IP and name already match"
	}
	if h.audit != nil {
		h.audit.Record(r, "network.clients.fixed_ip", req.MAC, status, detail)
	}
	if err != nil {
		h.writeFixedIPError(w, err, result)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"action": "unifi_client_fixed_ip", "result": result, "message": detail})
}

func (h *Handler) writeFixedIPError(w http.ResponseWriter, err error, result map[string]any) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, ErrFixedIPRefused):
		status = http.StatusBadRequest
	case errors.Is(err, ErrClientNotFound):
		status = http.StatusNotFound
	case errors.Is(err, unifi.ErrNotConfigured):
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"error": err.Error(), "result": result})
}
