package actions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Annotation keys stamped on a PipelineRun or Job an approval execution creates.
// Values live in annotations: a params hash is 64 hex characters, and a label
// value cannot hold that without being truncated.
const (
	AnnApprovalID = "bifrost.io/approval-id"
	AnnAttempt    = "bifrost.io/approval-attempt"
	AnnParamsHash = "bifrost.io/params-hash"
)

type paramsHashKey struct{}

// WithParamsHash attaches the approval's full params hash. Adoption checks it
// against the object; a missing hash is not a match.
func WithParamsHash(ctx context.Context, hash string) context.Context {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return ctx
	}
	return context.WithValue(ctx, paramsHashKey{}, hash)
}

// ParamsHashFrom returns the hash stored by WithParamsHash.
func ParamsHashFrom(ctx context.Context) (string, bool) {
	h, ok := ctx.Value(paramsHashKey{}).(string)
	return h, ok && h != ""
}

// IdentityAnnotations is the full approval id, the attempt, and the params
// hash. Nil when this call is not an approval execution.
func IdentityAnnotations(ctx context.Context) map[string]string {
	att, ok := CreateAttemptFrom(ctx)
	if !ok {
		return nil
	}
	attempt := att.Attempt
	if attempt < 1 {
		attempt = 1
	}
	out := map[string]string{
		AnnApprovalID: att.ApprovalID,
		AnnAttempt:    strconv.Itoa(attempt),
	}
	if hash, ok := ParamsHashFrom(ctx); ok {
		out[AnnParamsHash] = hash
	}
	return out
}

// Adopt reports whether an existing object belongs to this approval attempt.
// A direct call (no approval on the context) is refused: there is no identity
// to verify, so the object is not claimed. An approval execution must match
// the full approval id, the attempt, and the params hash; anything else is
// uncertain and must not be reported as executed.
func Adopt(ctx context.Context, namespace, name string, anns map[string]string) error {
	ref := namespace + "/" + name
	att, ok := CreateAttemptFrom(ctx)
	if !ok {
		return fmt.Errorf("conflicting object %s", ref)
	}
	hash, hashOK := ParamsHashFrom(ctx)
	attempt := att.Attempt
	if attempt < 1 {
		attempt = 1
	}
	if anns == nil {
		anns = map[string]string{}
	}
	if !hashOK || anns[AnnApprovalID] != att.ApprovalID || anns[AnnAttempt] != strconv.Itoa(attempt) || anns[AnnParamsHash] != hash {
		return Uncertain(fmt.Sprintf("conflicting object %s does not match this approval; outcome needs checking", ref))
	}
	return nil
}
