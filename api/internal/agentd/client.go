package agentd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Client calls the platform's agentrt routes (design v5 §5.2 路由草图) as
// this host's agentd: every body names the host, and the token is bound to
// it. The token is sent and never logged or put in an error.
type Client struct {
	Base  string // .../api/v1/agentrt
	Host  string
	Token string
	HTTP  *http.Client
}

// The platform's refusals agentd acts on (409 and its code), and a platform
// it cannot use now (unreachable, 5xx, 503): the latter is retried, the
// former decide what happens to the attempt.
var (
	ErrFenced      = errors.New("agentd: the platform says this attempt's lease is not ours")
	ErrKeyConflict = errors.New("agentd: an idempotency key reused with another request")
	ErrInGrace     = errors.New("agentd: the attempt is still within its grace")
	ErrState       = errors.New("agentd: not allowed in the attempt's state")
	ErrUnavailable = errors.New("agentd: the platform is unavailable")
)

// Tool is an executor this host runs, with the model provider it uses.
type Tool struct {
	Executor string `json:"executor"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

// Claimed is a leased attempt.
type Claimed struct {
	TaskID         string    `json:"task_id"`
	AttemptID      string    `json:"attempt_id"`
	AttemptNo      int       `json:"attempt_no"`
	MissionID      string    `json:"mission_id"`
	Role           string    `json:"role"`
	Repo           string    `json:"repo"`
	SpecArtifact   string    `json:"spec_artifact_id"`
	BaseCommit     string    `json:"base_commit"`
	PushedRef      string    `json:"pushed_ref"`
	EffectClass    string    `json:"effect_class"`
	WorkID         string    `json:"work_id"`
	Epoch          int64     `json:"lease_epoch"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	HardLimitS     int       `json:"hard_limit_s"`
	Tool           Tool      `json:"tool"`
	// FailedChecks are the checks the task's earlier rounds failed, for an
	// implementation in a round after the first (TD-405, lastround.go); a
	// platform from before it sends none.
	FailedChecks []FailedCheck `json:"failed_checks,omitempty"`
}

// FailedCheck is a check an earlier round of the task failed: deterministic
// or review, the attempt it checked, and the artifact of its report.
type FailedCheck struct {
	Method    string `json:"method"`
	AttemptNo int    `json:"attempt_no"`
	Artifact  string `json:"artifact_id"`
}

// Result is what an attempt hands over: its report, other artifacts, and the
// branch it pushed, if any.
type Result struct {
	Report       string   `json:"report,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
	PushedRef    string   `json:"pushed_ref,omitempty"`
	PushedCommit string   `json:"pushed_commit,omitempty"`
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 40 * time.Second}
}

// call sends one request and decodes a JSON answer into out (nil: ignore).
// It returns the status for callers that tell 200 from 204.
func (c *Client) call(ctx context.Context, method, path string, body io.Reader, header map[string]string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %s %s: %v", ErrUnavailable, method, path, redact(err.Error(), c.Token))
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("%w: %s %s: %v", ErrUnavailable, method, path, err)
	}
	switch {
	case resp.StatusCode == http.StatusOK && out != nil:
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("agentd: %s %s: %w", method, path, err)
		}
		return resp.StatusCode, nil
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent:
		return resp.StatusCode, nil
	}
	var e struct {
		Code    string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := fmt.Sprintf("%s %s: %d %s %s", method, path, resp.StatusCode, e.Code, e.Message)
	switch {
	case resp.StatusCode == http.StatusConflict && e.Code == "fenced":
		return resp.StatusCode, fmt.Errorf("%w (%s)", ErrFenced, detail)
	case resp.StatusCode == http.StatusConflict && e.Code == "key_conflict":
		return resp.StatusCode, fmt.Errorf("%w (%s)", ErrKeyConflict, detail)
	case resp.StatusCode == http.StatusConflict && e.Code == "in_grace":
		return resp.StatusCode, fmt.Errorf("%w (%s)", ErrInGrace, detail)
	case resp.StatusCode == http.StatusConflict:
		return resp.StatusCode, fmt.Errorf("%w (%s)", ErrState, detail)
	case resp.StatusCode >= 500:
		return resp.StatusCode, fmt.Errorf("%w (%s)", ErrUnavailable, detail)
	}
	return resp.StatusCode, fmt.Errorf("agentd: %s", detail)
}

const maxBody = 1 << 20

func redact(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "[token]")
}

func (c *Client) post(ctx context.Context, path string, body map[string]any, out any) (int, error) {
	if body == nil {
		body = map[string]any{}
	}
	body["host_id"] = c.Host
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	return c.call(ctx, http.MethodPost, path, bytes.NewReader(b), nil, out)
}

// Benchmark hands in a benchmark run (agentd bench) as this host and returns
// the platform's record of it.
func (c *Client) Benchmark(ctx context.Context, m BenchMeta, results []BenchResult) (map[string]any, error) {
	body := map[string]any{"run_id": m.RunID, "executor": m.Executor, "provider": m.Provider, "questions_sha256": m.Questions,
		"question_ids": m.QuestionIDs, "started_at": m.StartedAt.UTC().Format(time.RFC3339Nano), "results": results}
	if m.Model != "" {
		body["model"] = m.Model
	}
	var out map[string]any
	_, err := c.post(ctx, "/benchmark", body, &out)
	return out, err
}

// ExamPaper asks for a paper of job's exam for executor, as this host (agentd
// exam). The platform answers 409 while the executor has a paper open, and
// never reads one back to an Agent host: agentd keeps the paper it was
// handed.
func (c *Client) ExamPaper(ctx context.Context, job, executor string) (*store.ExamPaper, error) {
	var p store.ExamPaper
	q := url.Values{"host_id": {c.Host}, "executor": {executor}}
	if _, err := c.call(ctx, http.MethodGet, "/exams/"+url.PathEscape(job)+"?"+q.Encode(), nil, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ExamAnswers hands in a part of a run's answers as this host and returns
// the platform's counts.
func (c *Client) ExamAnswers(ctx context.Context, sub store.ExamSubmission) (map[string]any, error) {
	b, err := json.Marshal(struct {
		HostID string `json:"host_id"`
		store.ExamSubmission
	}{c.Host, sub})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	_, err = c.call(ctx, http.MethodPost, "/exams", bytes.NewReader(b), nil, &out)
	return out, err
}

// Claim asks for one attempt; nil when the platform had none for the whole
// long poll (204).
func (c *Client) Claim(ctx context.Context, freeSlots int, tools []Tool) (*Claimed, error) {
	var cl Claimed
	code, err := c.post(ctx, "/claim", map[string]any{"free_slots": freeSlots, "tools": tools}, &cl)
	if err != nil || code == http.StatusNoContent {
		return nil, err
	}
	return &cl, nil
}

func (c *Client) attempt(ctx context.Context, id, action string, epoch int64, body map[string]any, out any) error {
	if body == nil {
		body = map[string]any{}
	}
	if epoch != 0 {
		body["lease_epoch"] = epoch
	}
	_, err := c.post(ctx, "/attempts/"+id+"/"+action, body, out)
	return err
}

// ActionsUnsupported starts the refusal Heartbeat reports when the platform
// does not take actions at all (a platform from before W-31 2.3).
const ActionsUnsupported = "the platform does not take actions"

// Heartbeat renews the lease and carries the executor's recent actions, if
// any (W-31 2.3); stop is the platform asking the attempt to stop, refused
// why the platform kept none of the actions (the lease is renewed anyway).
// A platform from before 2.3 refuses a body with actions (400, unknown
// field): the heartbeat is sent again at once without them, and refused
// starts with ActionsUnsupported, so the lease never depends on actions
// while the PROD platform and agentd are on different versions.
func (c *Client) Heartbeat(ctx context.Context, attempt string, epoch int64, acts []actions.Action) (stop bool, expires time.Time, refused string, err error) {
	var out struct {
		Stop    bool      `json:"stop"`
		Expires time.Time `json:"lease_expires_at"`
		Refused string    `json:"actions_refused"`
	}
	body := map[string]any{}
	if epoch != 0 {
		body["lease_epoch"] = epoch
	}
	if len(acts) > 0 {
		body["actions"] = acts
	}
	code, err := c.post(ctx, "/attempts/"+attempt+"/heartbeat", body, &out)
	// only the refusal of the field itself: a 400 for another reason is
	// the caller's to see (review 267 B6)
	if err != nil && len(acts) > 0 && code == http.StatusBadRequest && strings.Contains(err.Error(), `unknown field "actions"`) {
		first := err
		delete(body, "actions")
		out.Refused = ""
		if _, err = c.post(ctx, "/attempts/"+attempt+"/heartbeat", body, &out); err == nil {
			out.Refused = ActionsUnsupported + ": " + oneLine(first.Error(), 200)
		}
	}
	return out.Stop, out.Expires, out.Refused, err
}

// Start records that the executor started, with the model it uses.
func (c *Client) Start(ctx context.Context, attempt string, epoch int64, model, provider string) error {
	return c.attempt(ctx, attempt, "start", epoch, map[string]any{"model": model, "model_provider": provider}, nil)
}

// Checkpoint records sample seq's artifacts (a retry of one sample reuses
// its seq).
func (c *Client) Checkpoint(ctx context.Context, attempt string, epoch int64, seq int, artifacts []string, tooLarge bool) error {
	return c.attempt(ctx, attempt, "checkpoint", epoch, map[string]any{"checkpoint_seq": seq, "artifacts": artifacts, "too_large": tooLarge}, nil)
}

// UsageUnsupported starts what Computed reports when the platform does not
// take usage at all (a platform from before W-31 2.4).
const UsageUnsupported = "the platform does not take usage"

// Computed records that the executor exited and how, with what it used and
// what its vendor said about its pools (W-31 2.4 slice ①). A platform from
// before 2.4 refuses a body with them (400, unknown field): computed is sent
// again at once without them, so an ending never depends on its usage while
// the PROD platform and agentd are on different versions. refused says why
// the platform kept no usage (the attempt is computed anyway).
func (c *Client) Computed(ctx context.Context, attempt string, epoch int64, exitCode *int, endHow, conclusion string, m Measured) (refused string, err error) {
	body := map[string]any{"exit_code": exitCode, "end_how": endHow, "conclusion": conclusion, "lease_epoch": epoch}
	if epoch == 0 {
		delete(body, "lease_epoch")
	}
	var dropped string
	if !m.empty() {
		// a report that cannot even be encoded is left out, never the ending
		// (review 274 A1)
		if _, merr := json.Marshal(map[string]any{"usage": m.Usage, "pools": m.Pools}); merr != nil {
			dropped, m = "usage not sent: "+oneLine(merr.Error(), 200), Measured{}
		}
	}
	if m.Usage != nil {
		body["usage"] = m.Usage
	}
	if len(m.Pools) > 0 {
		body["pools"] = m.Pools
	}
	var out struct {
		Refused string `json:"usage_refused"`
	}
	path := "/attempts/" + attempt + "/computed"
	code, err := c.post(ctx, path, body, &out)
	if err != nil && !m.empty() && code == http.StatusBadRequest &&
		(strings.Contains(err.Error(), `unknown field "usage"`) || strings.Contains(err.Error(), `unknown field "pools"`)) {
		first := err
		delete(body, "usage")
		delete(body, "pools")
		if _, err = c.post(ctx, path, body, &out); err == nil {
			out.Refused = UsageUnsupported + ": " + oneLine(first.Error(), 200)
		}
	}
	if dropped != "" && err == nil {
		out.Refused = dropped
	}
	return out.Refused, err
}

func (r Result) body() map[string]any {
	b := map[string]any{}
	if r.Report != "" {
		b["report"] = r.Report
	}
	if len(r.Artifacts) > 0 {
		b["artifacts"] = r.Artifacts
	}
	if r.PushedRef != "" {
		b["pushed_ref"], b["pushed_commit"] = r.PushedRef, r.PushedCommit
	}
	return b
}

// Persist hands the result over.
func (c *Client) Persist(ctx context.Context, attempt string, epoch int64, r Result) error {
	return c.attempt(ctx, attempt, "persist", epoch, r.body(), nil)
}

// Submit asks for the persisted result to be checked.
func (c *Client) Submit(ctx context.Context, attempt string, epoch int64) error {
	return c.attempt(ctx, attempt, "submit", epoch, nil, nil)
}

// Salvage hands over what an attempt whose lease is lost left here.
func (c *Client) Salvage(ctx context.Context, attempt string, r Result) error {
	return c.attempt(ctx, attempt, "salvage", 0, r.body(), nil)
}

// Ended records that the attempt's process group is gone from this host.
func (c *Client) Ended(ctx context.Context, attempt, endHow string, groupCleared bool) error {
	return c.attempt(ctx, attempt, "ended", 0, map[string]any{"end_how": endHow, "group_cleared": groupCleared}, nil)
}

// ArtifactID is the content address of body.
func ArtifactID(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PutArtifact uploads body as kind and returns its id.
func (c *Client) PutArtifact(ctx context.Context, kind, mediaType string, body []byte) (string, error) {
	id := ArtifactID(body)
	_, err := c.call(ctx, http.MethodPut, "/artifacts/"+id, bytes.NewReader(body),
		map[string]string{"X-Artifact-Kind": kind, "Content-Type": mediaType}, nil)
	return id, err
}

// GetArtifact downloads an artifact (a task spec) and checks its hash.
func (c *Client) GetArtifact(ctx context.Context, id string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+"/artifacts/"+id, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: GET artifact: %v", ErrUnavailable, redact(err.Error(), c.Token))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("%w: GET artifact: %v", ErrUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agentd: GET artifact %s: %d", id, resp.StatusCode)
	}
	if ArtifactID(body) != id {
		return nil, fmt.Errorf("agentd: artifact %s does not match its content", id)
	}
	return body, nil
}
