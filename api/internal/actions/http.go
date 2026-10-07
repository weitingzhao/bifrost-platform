package actions

import (
	"encoding/json"
	"net/http"
)

// HandleList serves GET /api/v1/actions.
func HandleList(w http.ResponseWriter, _ *http.Request) {
	list := Catalog()
	views := make([]View, 0, len(list))
	for _, a := range list {
		views = append(views, a.View())
	}
	writeJSON(w, http.StatusOK, views)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
