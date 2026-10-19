package agentrtapi

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A host hands in a benchmark run (W-31 2.4 slice ⑤; store/benchmark.go): an
// agentd token, for the host it is bound to, as a claim is.
type benchmarkBody struct {
	hostBody
	store.BenchmarkRun
}

func (a *API) recordBenchmark(w http.ResponseWriter, r *http.Request) {
	var b benchmarkBody
	if !decode(w, r, &b) || !boundHost(w, r, b.HostID) {
		return
	}
	// A host that never claimed is registered as a claim would register it.
	if err := store.RegisterAgentdHost(r.Context(), a.DB, b.HostID, actuation.PrincipalFromContext(r.Context()).Name); err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.RecordBenchmark(r.Context(), a.DB, b.HostID, b.BenchmarkRun)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
