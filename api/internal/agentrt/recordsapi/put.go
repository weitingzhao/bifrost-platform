package recordsapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
)

// The artifact kinds agentd may upload: results, not specs or plans.
var agentdKinds = map[string]bool{"report": true, "verify": true, "review": true, "log_tail": true,
	"checkpoint_patch": true, "handover": true, "other": true}

// HandlePutArtifact stores a body under its sha256 (PUT /artifacts/{sha256}): the
// id in the path must be the hash of the body; the kind comes in the
// X-Artifact-Kind header and must be one the schema has; an artifact with a
// local path in it is refused unless its kind is one H8 lets through
// (artifact.LocalPath). The first upload of a body records artifact_put, as
// the writer (agentd as its host, others as for proposals); uploading it
// again is harmless.
func (rt Routes) HandlePutArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p := actuation.PrincipalFromContext(r.Context())
	host, isAgentd := actuation.AgentdHost(p)
	kind := r.Header.Get("X-Artifact-Kind")
	if rt.Host.LimitedReporter(p) && kind != "other" {
		rt.Host.Error(w, http.StatusForbidden, "forbidden", "a reporter may only upload an external work description")
		return
	}
	if p.Role == actuation.RoleAgentd && !isAgentd {
		rt.Host.Error(w, http.StatusForbidden, "forbidden", "an agentd token must be bound to a host")
		return
	}
	if !artifact.ArtifactKinds[kind] {
		rt.Host.Error(w, http.StatusBadRequest, "kind", fmt.Sprintf("X-Artifact-Kind %q is not an artifact kind", kind))
		return
	}
	if isAgentd && !agentdKinds[kind] {
		rt.Host.Error(w, http.StatusForbidden, "kind", "agentd may not upload artifacts of kind "+kind)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		rt.Host.Error(w, http.StatusRequestEntityTooLarge, "too_large", "an artifact is at most 1 MiB")
		return
	}
	if rt.Host.LimitedReporter(p) {
		if err := rt.Host.ExternalDescription(body); err != nil {
			rt.Host.Error(w, http.StatusForbidden, "forbidden", "a reporter may only upload an external work description")
			return
		}
	}
	sum := sha256.Sum256(body)
	if id != "sha256:"+hex.EncodeToString(sum[:]) {
		rt.Host.Error(w, http.StatusBadRequest, "hash", "the path is not the sha256 of the body")
		return
	}
	if p, found := artifact.LocalPath(kind, body); found {
		rt.refuse(w, r, host, kind, id, "local_path", "a local path in the artifact: "+p)
		return
	}
	if ref, found := artifact.WorkspaceRef(kind, body); found {
		rt.refuse(w, r, host, kind, id, "workspace_ref", "the artifact refers to a task's workspace: "+ref)
		return
	}
	media := r.Header.Get("Content-Type")
	if media == "" {
		media = "application/octet-stream"
	}
	who, err := rt.Host.ActorOf(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	_, created, err := artifact.UploadArtifact(r.Context(), rt.Host.DB(), who, kind, media, body)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"artifact_id": id, "created": created})
}

// refuse answers an artifact refused by rule (422). A report agentd uploads
// is recorded as refused on its host's stream (artifact.RecordRefusal, TD-382),
// and an attempt of the host that then ends without its result says so; the
// answer is the refusal whatever the record does.
func (rt Routes) refuse(w http.ResponseWriter, r *http.Request, host, kind, id, rule, message string) {
	if host != "" {
		if _, err := artifact.RecordRefusal(r.Context(), rt.Host.DB(), host, kind, id, rule); err != nil {
			rt.Host.Logf("agentrt: the refusal of %s from host %s was not recorded: %v", id, host, err)
		}
	}
	rt.Host.Error(w, http.StatusUnprocessableEntity, rule, message)
}
