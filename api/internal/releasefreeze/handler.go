package releasefreeze

import (
	"net/http"
)

// UnfreezeGoneBody is the fixed body of the retired unfreeze route.
// Lifting is not this route: the Owner deletes the record with release.sh unfreeze.
const UnfreezeGoneBody = `{"message":"lifting is done by the Owner with release.sh unfreeze"}`

// UnfreezeHandler answers 410 Gone. It is not registered on a router in this slice.
func UnfreezeHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	if _, err := w.Write([]byte(UnfreezeGoneBody)); err != nil {
		return
	}
}
