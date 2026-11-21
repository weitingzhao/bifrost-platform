package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

func decodeView(w http.ResponseWriter, r *http.Request, out any) bool {
	_, ok := decodeViewRaw(w, r, out)
	return ok
}

// decodeViewRaw is decodeView, giving the body it read too.
func decodeViewRaw(w http.ResponseWriter, r *http.Request, out any) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err == nil {
		_, err = canon.Parse(raw)
	}
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(out)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "body: "+err.Error())
		return nil, false
	}
	return raw, true
}
