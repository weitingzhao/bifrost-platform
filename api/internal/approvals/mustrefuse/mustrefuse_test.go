package mustrefuse_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// caseCount is how many cases the set has. Cases are added, with this number;
// a case is never dropped to make a change pass.
const caseCount = 12

const goodReason = "x\n改了什么：一个对象\n批了以后谁因此能做什么新事：没有新能力"

type world struct {
	t   *testing.T
	h   http.Handler
	svc *approvals.Service
}

func newWorld(t *testing.T) *world {
	t.Helper()
	t.Setenv("OPS_VIEWER_ENV", "prod") // human requests are created and decided on the PROD platform
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.yaml")
	if err := os.WriteFile(authPath, []byte(`
tokens:
  - {name: viewer, role: viewer, token: viewer-tok}
  - {name: operator, role: operator, token: operator-tok}
  - {name: admin, role: admin, token: admin-tok}
  - {name: exec, role: executor, token: executor-tok}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(authPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := approvals.New(filepath.Join(dir, "approvals"), nil)
	r := chi.NewRouter()
	approvals.Mount(r, auth, svc)
	return &world{t: t, h: r, svc: svc}
}

func (w *world) call(method, path, body, token, session string) *httptest.ResponseRecorder {
	w.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if session != "" {
		req.Header.Set(approvals.SessionHeader, session)
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	return rec
}

// card builds an execution card as session and returns its id.
func (w *world) card(session, reason string) (id string, res *httptest.ResponseRecorder) {
	w.t.Helper()
	body, _ := json.Marshal(map[string]any{"action": "owner_run_command", "reason": reason,
		"params": map[string]any{"command": "true", "reason": "r", "runner": "host"}})
	res = w.call(http.MethodPost, "/approvals", string(body), "operator-tok", session)
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return out.ID, res
}

// approve takes the card through the admin's approval, reading the line and
// hash the way a page does.
func (w *world) approve(id string) *httptest.ResponseRecorder {
	w.t.Helper()
	got := w.call(http.MethodGet, "/approvals/"+id, "", "viewer-tok", "")
	var row struct {
		ApprovalLine string `json:"approval_line"`
		ParamsHash   string `json:"params_hash"`
		Number       int    `json:"number"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &row); err != nil || row.ApprovalLine == "" {
		w.t.Fatalf("read card = %d %s", got.Code, got.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"channel": "console", "confirm_number": row.Number, "approval_line": row.ApprovalLine, "params_hash": row.ParamsHash})
	return w.call(http.MethodPost, "/approvals/"+id+"/approve", string(body), "admin-tok", "")
}

func (w *world) status(id string) string {
	w.t.Helper()
	got := w.call(http.MethodGet, "/approvals/"+id, "", "viewer-tok", "")
	var row struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(got.Body.Bytes(), &row)
	return row.Status
}

// mustRefuse is one thing the service must refuse, with where it comes from.
type mustRefuse struct {
	name   string
	origin string
	run    func(w *world) string // "" when the service refused as it must; otherwise what it did
}

func code(rec *httptest.ResponseRecorder, want ...int) string {
	for _, c := range want {
		if rec.Code == c {
			return ""
		}
	}
	return rec.Result().Status + " " + rec.Body.String()
}

var cases = []mustRefuse{
	{"an execution card with --raw under a made-up subcommand, or a dry run that is not the server's, is not built", "#550 and 2026-11-17: --raw is read by position, only with get; a client dry run stays refused whatever wraps it", func(w *world) string {
		for _, command := range []string{"kubectl -n get apply2 --raw /x", "kubectl delete --raw /x --dry-run=server", `D="${V:+--dry-run=client}"; kubectl apply -f x ${D}`} {
			body, _ := json.Marshal(map[string]any{"action": "owner_run_command", "reason": goodReason,
				"params": map[string]any{"command": command, "reason": "r"}})
			if msg := code(w.call(http.MethodPost, "/approvals", string(body), "operator-tok", "s1"), http.StatusBadRequest); msg != "" {
				return command + ": " + msg
			}
		}
		return ""
	}},
	{"an agent cannot approve a card", "ADR section 5; Owner 2026-10-07: approval is the Owner's", func(w *world) string {
		id, _ := w.card("s1", goodReason)
		rec := w.call(http.MethodPost, "/approvals/"+id+"/approve", `{"channel":"chat"}`, "operator-tok", "s1")
		if rec.Code < 400 || w.status(id) != "pending" {
			return "operator approve = " + rec.Result().Status + ", card " + w.status(id)
		}
		return ""
	}},
	{"an agent cannot reject or resolve a card", "ADR section 5: the Owner decides", func(w *world) string {
		id, _ := w.card("s1", goodReason)
		for _, p := range []string{"reject", "resolve"} {
			if rec := w.call(http.MethodPost, "/approvals/"+id+"/"+p, `{"reason":"r"}`, "operator-tok", "s1"); rec.Code < 400 {
				return "operator " + p + " = " + rec.Result().Status
			}
		}
		return ""
	}},
	{"an executor cannot create, approve or reject", "execution card 4: the executor only claims, heartbeats and reports", func(w *world) string {
		id, _ := w.card("s1", goodReason)
		for _, c := range []struct{ path, body string }{
			{"/approvals", `{"action":"cordon_node","params":{"name":"n"},"reason":"r"}`},
			{"/approvals/" + id + "/approve", `{}`},
			{"/approvals/" + id + "/reject", `{"reason":"r"}`},
		} {
			if rec := w.call(http.MethodPost, c.path, c.body, "executor-tok", ""); rec.Code != http.StatusUnauthorized {
				return c.path + " = " + rec.Result().Status
			}
		}
		return ""
	}},
	{"a card cannot be withdrawn by another session", "#562, #564, #566 (2026-11-14); Owner 2026-11-15", func(w *world) string {
		id, _ := w.card("s1", goodReason)
		if msg := code(w.call(http.MethodPost, "/approvals/"+id+"/withdraw", `{"reason":"mine"}`, "operator-tok", "s2"), http.StatusForbidden); msg != "" {
			return msg
		}
		if w.status(id) != "pending" {
			return "card " + w.status(id)
		}
		return ""
	}},
	{"a claimed card cannot be withdrawn", "an executor that holds a lease may be running it", func(w *world) string {
		id, _ := w.card("s1", goodReason)
		if rec := w.approve(id); rec.Code != http.StatusAccepted {
			return "approve = " + rec.Result().Status + " " + rec.Body.String()
		}
		if rec := w.call(http.MethodPost, "/approvals/claim", `{"executor_id":"x","runners":["host"]}`, "executor-tok", ""); rec.Code != http.StatusOK {
			return "claim = " + rec.Result().Status
		}
		return code(w.call(http.MethodPost, "/approvals/"+id+"/withdraw", `{"reason":"late"}`, "operator-tok", "s1"), http.StatusConflict)
	}},
	{"a withdrawn card cannot be approved or claimed", "withdrawing only narrows; it must not leave a card that can still run", func(w *world) string {
		id, _ := w.card("s1", goodReason)
		if rec := w.call(http.MethodPost, "/approvals/"+id+"/withdraw", `{"reason":"moot"}`, "operator-tok", "s1"); rec.Code != http.StatusOK {
			return "withdraw = " + rec.Result().Status
		}
		if rec := w.approve(id); rec.Code == http.StatusAccepted {
			return "approved a withdrawn card"
		}
		if rec := w.call(http.MethodPost, "/approvals/claim", `{"executor_id":"x","runners":["host"]}`, "executor-tok", ""); rec.Code != http.StatusNoContent {
			return "claim after withdrawal = " + rec.Result().Status
		}
		return ""
	}},
	{"a new card does not replace another requester's card", "#562: a replacement is the requester's own; anyone else's card still blocks", func(w *world) string {
		w.svc.SetOpenKey(func(action string, params map[string]any) string {
			if action == "cordon_node" {
				return "node:n1"
			}
			return ""
		})
		make := func(session string) *httptest.ResponseRecorder {
			return w.call(http.MethodPost, "/approvals", `{"action":"cordon_node","params":{"name":"n1"},"reason":"r"}`, "operator-tok", session)
		}
		if rec := make("s1"); rec.Code != http.StatusCreated {
			return "first = " + rec.Result().Status
		}
		return code(make("s2"), http.StatusConflict)
	}},
	{"a tier D request is never auto-approved", "the signed policy never judges tier D (releasepolicy engine: tier D is never auto-approved)", func(w *world) string {
		w.svc.SetAutoApprover(func(_ context.Context, _ string, _ actions.Tier, _ map[string]any) string {
			return "policy-that-approves-everything"
		})
		_, rec := w.card("s1", goodReason)
		var out struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != http.StatusCreated || out.Status != "pending" {
			return "tier D card with an approving policy = " + rec.Result().Status + " " + rec.Body.String()
		}
		return ""
	}},
	{"an approval with another hash than the card's is refused", "the Owner approves exactly the text shown; params_hash is that text's fingerprint", func(w *world) string {
		id, _ := w.card("s1", goodReason)
		got := w.call(http.MethodGet, "/approvals/"+id, "", "viewer-tok", "")
		var row struct {
			ApprovalLine string `json:"approval_line"`
			Number       int    `json:"number"`
		}
		_ = json.Unmarshal(got.Body.Bytes(), &row)
		body, _ := json.Marshal(map[string]any{"channel": "console", "confirm_number": row.Number, "approval_line": row.ApprovalLine, "params_hash": strings.Repeat("0", 64)})
		rec := w.call(http.MethodPost, "/approvals/"+id+"/approve", string(body), "admin-tok", "")
		if rec.Code == http.StatusAccepted || w.status(id) != "pending" {
			return "approved with a wrong hash = " + rec.Result().Status
		}
		return ""
	}},
	{"an execution card without the two lines is not built", "three-questions review 2026-11-15: the Owner decides in about a minute", func(w *world) string {
		_, rec := w.card("s1", "plain reason")
		return code(rec, http.StatusBadRequest)
	}},
	{"an execution card with --raw after a write verb is not built", "#550: a trial run with --raw really deleted; --raw is a read only after get (2026-11-17)", func(w *world) string {
		body, _ := json.Marshal(map[string]any{"action": "owner_run_command", "reason": goodReason,
			"params": map[string]any{"command": "kubectl delete --raw /x", "reason": "r"}})
		return code(w.call(http.MethodPost, "/approvals", string(body), "operator-tok", "s1"), http.StatusBadRequest)
	}},
}

func TestTheApprovalServiceStillRefuses(t *testing.T) {
	if len(cases) != caseCount {
		t.Fatalf("the set has %d cases and says %d: add cases with the number, never drop one to make a change pass", len(cases), caseCount)
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.name] || c.origin == "" {
			t.Fatalf("case %q is repeated or has no origin", c.name)
		}
		seen[c.name] = true
		t.Run(c.name, func(t *testing.T) {
			if why := c.run(newWorld(t)); why != "" {
				t.Fatalf("the service no longer refuses (%s): %s", c.origin, why)
			}
		})
	}
}
