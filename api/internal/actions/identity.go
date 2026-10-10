package actions

import (
	"context"
	"errors"
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

// WithParamsHash attaches the approval's full params hash so it can be
// stamped on the object this process creates. A missing hash is not stamped.
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
// hash, stamped on an object this process creates. Nil when this call is not
// an approval execution. They are how a person checks an object; they are
// not used to adopt one.
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

// ObjectExists is the outcome when the deterministic name is already taken,
// on the read before create and on AlreadyExists alike. The object is never
// adopted, including when its annotations match this approval. An approval
// execution becomes unknown; a direct call is a plain error. The message
// names the object and says a person must check it.
func ObjectExists(ctx context.Context, namespace, name string) error {
	msg := fmt.Sprintf("object %s/%s already exists; it must be checked by hand", namespace, name)
	if _, ok := CreateAttemptFrom(ctx); ok {
		return Uncertain(msg)
	}
	return errors.New(msg)
}
