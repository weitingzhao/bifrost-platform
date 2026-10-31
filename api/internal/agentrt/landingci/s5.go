package landingci

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules/s5check"
)

// Deployment-owned trust root, generated from reviewed infra manifests.
// The initial zero digest intentionally disables S5 until the image is built,
// reviewed, pinned in BOTH repositories and deployed through the core lane.
//
//go:embed s5trusted/contract.json
var s5ContractJSON []byte

type s5Contract struct {
	Schema      int                       `json:"schema"`
	Image       string                    `json:"image"`
	Pipeline    map[string]any            `json:"pipeline"`
	Tasks       map[string]map[string]any `json:"tasks"`
	RunTemplate map[string]any            `json:"runTemplate"`
	// Materials is the pinned image's `s5check --ci-materials` output. It is
	// generated with the image digest from the same reviewed build; absent
	// (as in the UNBUILT trust root) means S5 is unavailable.
	Materials *s5Materials `json:"materials,omitempty"`
}

type s5Materials struct {
	ProfileDigest   string `json:"profileDigest"`
	ToolchainDigest string `json:"toolchainDigest"`
	CallersDigest   string `json:"callersDigest"`
	TypesDigest     string `json:"typesDigest"`
	TestsDigest     string `json:"testsDigest"`
	UISHA           string `json:"uiSHA"`
}

const s5ImageRepository = "192.168.10.73:30500/bifrost-s5-checker@"

func loadS5Contract() (s5Contract, error) {
	var c s5Contract
	err := json.Unmarshal(s5ContractJSON, &c)
	return c, err
}

// deployedS5Contract is the production trust root: the embedded contract,
// usable only when its image is built, its materials pinned and its profile
// equal to the reviewed manifest compiled into this binary.
func deployedS5Contract() (s5Contract, error) {
	c, err := loadS5Contract()
	if err != nil {
		return c, err
	}
	return c, c.deployable()
}

func (c s5Contract) deployable() error {
	p, err := c.policy()
	if err != nil {
		return err
	}
	reviewed, err := s5check.EmbeddedPolicyDigest()
	if err != nil {
		return fmt.Errorf("S5_EVIDENCE_MISSING: reviewed policy manifest unreadable: %w", err)
	}
	if p.ProfileDigest != reviewed {
		return fmt.Errorf("S5_CI_IDENTITY: checker profile is not the reviewed manifest")
	}
	return nil
}

// policy derives the deployment-owned S5 material. It never reads R, a rule
// extension, an artifact or a request. UNBUILT or unpinned fails closed.
func (c s5Contract) policy() (landing.S5Policy, error) {
	if c.Schema != 1 || len(c.Tasks) != 2 || !strings.HasPrefix(c.Image, s5ImageRepository) {
		return landing.S5Policy{}, fmt.Errorf("S5_CI_IDENTITY: checker is not the deployed trust root")
	}
	image := strings.TrimPrefix(c.Image, s5ImageRepository)
	if !strings.HasPrefix(image, "sha256:") || !nonzeroHex(strings.TrimPrefix(image, "sha256:"), 64) {
		return landing.S5Policy{}, fmt.Errorf("S5_CI_IDENTITY: checker image UNBUILT")
	}
	m := c.Materials
	if m == nil {
		return landing.S5Policy{}, fmt.Errorf("S5_EVIDENCE_MISSING: checker materials are not pinned")
	}
	for _, d := range []string{m.ProfileDigest, m.ToolchainDigest, m.CallersDigest, m.TypesDigest, m.TestsDigest} {
		if !s5Hex.MatchString(d) || !nonzeroHex(d, 64) {
			return landing.S5Policy{}, fmt.Errorf("S5_EVIDENCE_MISSING: invalid pinned checker material")
		}
	}
	if !fullSHA.MatchString(m.UISHA) || !nonzeroHex(m.UISHA, 40) {
		return landing.S5Policy{}, fmt.Errorf("S5_EVIDENCE_MISSING: UI revision is not pinned")
	}
	// Only the freeze check may run with a token, and it must get one; every
	// S5 task runs as bifrost-s5 without one (Tekton merges, so check merged).
	specs := sliceS5(c.RunTemplate["taskRunSpecs"])
	first, _ := firstS5(specs)["pipelineTaskName"].(string)
	freezeSA, freezePod := s5TaskRunTemplate(c, "freeze-check")
	if len(specs) != 1 || first != "freeze-check" || freezeSA != "default" || freezePod["automountServiceAccountToken"] != true {
		return landing.S5Policy{}, fmt.Errorf("S5_CI_IDENTITY: freeze check template")
	}
	for _, stage := range []string{"s5-fetch", "s5-check"} {
		if sa, pod := s5TaskRunTemplate(c, stage); sa != "bifrost-s5" || pod["automountServiceAccountToken"] != false {
			return landing.S5Policy{}, fmt.Errorf("S5_CI_IDENTITY: S5 task template")
		}
	}
	p := landing.S5Policy{ProfileDigest: m.ProfileDigest, CheckerImageDigest: image, ToolchainDigest: m.ToolchainDigest,
		CallersDigest: m.CallersDigest, TypesDigest: m.TypesDigest, TestsDigest: m.TestsDigest, UISHA: m.UISHA}
	if !p.Valid() {
		return landing.S5Policy{}, fmt.Errorf("S5_EVIDENCE_MISSING: invalid pinned checker material")
	}
	return p, nil
}

func (c s5Contract) validate(k S5CIKey) error {
	if err := validateS5Key(k); err != nil {
		return err
	}
	if c.Schema != 1 || c.Image != s5ImageRepository+k.CheckerImageDigest || len(c.Tasks) != 2 {
		return fmt.Errorf("S5_CI_IDENTITY: checker is not the deployed trust root")
	}
	// The key's material must be exactly the deployed material: a key built
	// under an older deployment is never run or read under a newer one.
	p, err := c.policy()
	if err != nil {
		return err
	}
	if k.ProfileDigest != p.ProfileDigest || k.ToolchainDigest != p.ToolchainDigest || k.CallersDigest != p.CallersDigest ||
		k.TypesDigest != p.TypesDigest || k.TestsDigest != p.TestsDigest || k.UISHA != p.UISHA {
		return fmt.Errorf("S5_CI_IDENTITY: key material is not the deployed checker material")
	}
	return nil
}

// S5RunIdentity must be persisted separately after EnsureS5 returns. A missing
// or replaced UID is never reconstructed from labels by ReadS5.
type S5RunIdentity struct {
	Run        string `json:"run"`
	UID        string `json:"uid"`
	KeyDigest  string `json:"keyDigest"`
	Generation uint64 `json:"generation"`
}

// S5Evidence is a CI transport result, NOT a main authorization. Line B must
// still verify current rules, ReviewBinding and PatchEvidence under its lock.
// Content is the canonical schema-2 ContentEvidence emitted by the image.
type S5Evidence struct {
	Identity S5RunIdentity
	Status   string
	Reason   string
	Content  json.RawMessage
	Digest   string
	// CreatedAt is the identity-checked run's server creation time;
	// CompletedAt is its completionTime and is set only on Succeeded.
	CreatedAt, CompletedAt time.Time
	// Retryable marks a terminal run that never reached the mandatory tasks
	// for an infrastructure reason (the release freeze check refused). It is
	// still a failure of this generation; only the operation owner may open
	// a new generation for it, within its bounds.
	Retryable bool
}

func s5Named(values map[string]string) []any {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]any{"name": key, "value": values[key]})
	}
	return out
}

func s5Same(a, b any) bool {
	x, e1 := canonicalS5(a)
	y, e2 := canonicalS5(b)
	return e1 == nil && e2 == nil && string(x) == string(y)
}

func s5RunSpec(k S5CIKey, contract s5Contract) map[string]any {
	b, _ := json.Marshal(contract.RunTemplate)
	var spec map[string]any
	_ = json.Unmarshal(b, &spec)
	spec["params"] = s5Named(s5Params(k))
	return spec
}

func s5SpecMatches(obj *unstructured.Unstructured, k S5CIKey, contract s5Contract) bool {
	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	params, counts := namedValues(*obj, "spec", "params")
	if !reflect.DeepEqual(params, s5Params(k)) {
		return false
	}
	for _, n := range counts {
		if n != 1 {
			return false
		}
	}
	// Parameter order is not semantic; everything else, including pod/SA,
	// workspaces, timeout, resolver and inline overrides, is pinned exactly
	// up to Tekton's inert round-trip (durations, empty template metadata).
	spec["params"] = s5Named(params)
	return s5Resolved(spec, s5RunSpec(k, contract))
}

func (c *Client) s5Dynamic() (dynamic.Interface, error) {
	if c.Dynamic == nil {
		return nil, fmt.Errorf("S5 CI transport unavailable")
	}
	d, err := c.Dynamic()
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("S5 CI transport unavailable")
	}
	return d, nil
}

func s5Resource(d dynamic.Interface, ns, resource string) dynamic.ResourceInterface {
	return d.Resource(schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: resource}).Namespace(ns)
}

func (c *Client) ensureS5(ctx context.Context, k S5CIKey, contract s5Contract) (S5RunIdentity, error) {
	if err := contract.validate(k); err != nil {
		return S5RunIdentity{}, err
	}
	name, _ := k.RunName()
	digest, _ := k.Digest()
	d, err := c.s5Dynamic()
	if err != nil {
		return S5RunIdentity{}, err
	}
	res := s5Resource(d, c.namespace(), "pipelineruns")
	identity := func(obj *unstructured.Unstructured) (S5RunIdentity, error) {
		if obj.GetName() != name || obj.GetNamespace() != c.namespace() || obj.GetUID() == "" || !s5SpecMatches(obj, k, contract) {
			return S5RunIdentity{}, fmt.Errorf("S5_CI_IDENTITY: run collision or missing server UID")
		}
		return S5RunIdentity{Run: name, UID: string(obj.GetUID()), KeyDigest: digest, Generation: k.Generation}, nil
	}
	if obj, e := res.Get(ctx, name, metav1.GetOptions{}); e == nil {
		return identity(obj)
	} else if !apierrors.IsNotFound(e) {
		return S5RunIdentity{}, e
	}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "spec": s5RunSpec(k, contract)}}
	obj.SetName(name)
	obj.SetNamespace(c.namespace())
	obj.SetLabels(map[string]string{"bifrost.io/purpose": "s5", "bifrost.io/repo": k.Repo, "tekton.dev/pipeline": "bifrost-ci-s5"})
	if created, e := res.Create(ctx, obj, metav1.CreateOptions{}); e == nil {
		return identity(created)
	} else {
		err = e
	}
	// Includes AlreadyExists and an uncertain response after successful creation.
	if existing, e := res.Get(ctx, name, metav1.GetOptions{}); e == nil {
		return identity(existing)
	}
	return S5RunIdentity{}, err
}

func (c *Client) readS5(ctx context.Context, k S5CIKey, saved S5RunIdentity, contract s5Contract) (S5Evidence, error) {
	out := S5Evidence{Identity: saved, Status: "Unknown", Reason: "S5_EVIDENCE_MISSING"}
	if err := contract.validate(k); err != nil {
		return out, err
	}
	name, _ := k.RunName()
	digest, _ := k.Digest()
	if saved.Run != name || saved.KeyDigest != digest || saved.Generation != k.Generation || saved.UID == "" {
		out.Reason = "S5_CI_IDENTITY"
		return out, nil
	}
	d, err := c.s5Dynamic()
	if err != nil {
		return out, err
	}
	// The run is selected by its exact name and the UID bound when it was
	// created, never by a list or label: another run (forged, inline, older
	// or newer) cannot be selected in its place. The operation stream owns
	// the generation, so a newer generation never reads this key at all.
	obj, err := s5Resource(d, c.namespace(), "pipelineruns").Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if string(obj.GetUID()) != saved.UID || !s5SpecMatches(obj, k, contract) || obj.GetDeletionTimestamp() != nil {
		out.Reason = "S5_CI_IDENTITY"
		return out, nil
	}
	out.CreatedAt = obj.GetCreationTimestamp().Time
	status := runStatus(*obj)
	if status != "Succeeded" && status != "Failed" {
		out.Status = status
		return out, nil
	}
	// The run is terminal: every rejection below (missing, skipped or
	// mismatched evidence) is final for this generation, never "still waiting".
	out.Status = "Failed"
	resolved, ok, _ := unstructured.NestedMap(obj.Object, "status", "pipelineSpec")
	if !ok || !s5Resolved(resolved, s5ResolvedPipeline(obj, contract)) {
		out.Reason = "S5_CI_IDENTITY"
		return out, nil
	}
	if status == "Failed" {
		reason, retryable, err := c.s5FailedRun(ctx, d, obj)
		out.Reason, out.Retryable = reason, retryable
		return out, err
	}
	if !s5Completed(*obj) {
		out.Reason = "S5_CI_IDENTITY"
		return out, nil
	}
	if skipped, ok, _ := unstructured.NestedSlice(obj.Object, "status", "skippedTasks"); ok && len(skipped) != 0 {
		out.Reason = "S5_EVIDENCE_MISSING"
		return out, nil
	}
	checkResults, reason, err := c.s5Tasks(ctx, d, obj, k, contract)
	if err != nil {
		return out, err
	}
	if reason != "" {
		out.Reason = reason
		return out, nil
	}
	values, counts := namedValues(*obj, "status", "results")
	if !reflect.DeepEqual(values, checkResults) {
		out.Reason = "S5_BINDING"
		return out, nil
	}
	for _, n := range counts {
		if n != 1 {
			out.Reason = "S5_CI_IDENTITY"
			return out, nil
		}
	}
	content, reason := s5CheckedResults(values, k, saved.UID)
	if reason != "" {
		// A bound, identity-checked rejection keeps the checker's own code
		// (§5): S5_BEHAVIOR, S5_CALLER_CHANGED, S5_TYPE_CONTRACT, ...
		out.Reason = reason
		return out, nil
	}
	end, _, _ := unstructured.NestedString(obj.Object, "status", "completionTime")
	completed, err := time.Parse(time.RFC3339Nano, end)
	if err != nil || out.CreatedAt.IsZero() || completed.Before(out.CreatedAt) {
		out.Reason = "S5_CI_IDENTITY"
		return out, nil
	}
	out.CompletedAt = completed
	out.Status, out.Reason, out.Content, out.Digest = "Succeeded", "", content, values["evidence-digest"]
	return out, nil
}

// s5ResolvedPipeline is the contract pipeline as Tekton stores it for this
// run: its own (already verified) parameters and context substituted.
func s5ResolvedPipeline(run *unstructured.Unstructured, contract s5Contract) any {
	params, _ := namedValues(*run, "spec", "params")
	return s5Substitute(contract.Pipeline, params, map[string]string{
		"pipelineRun.uid": string(run.GetUID()), "pipelineRun.name": run.GetName(), "pipelineRun.namespace": run.GetNamespace()})
}

func s5Completed(obj unstructured.Unstructured) bool {
	start, _, _ := unstructured.NestedString(obj.Object, "status", "startTime")
	end, _, _ := unstructured.NestedString(obj.Object, "status", "completionTime")
	a, e1 := time.Parse(time.RFC3339Nano, start)
	b, e2 := time.Parse(time.RFC3339Nano, end)
	return e1 == nil && e2 == nil && !a.IsZero() && !b.Before(a)
}

func s5ImageID(id, digest string) bool {
	return id == digest || strings.HasSuffix(id, "@"+digest)
}
