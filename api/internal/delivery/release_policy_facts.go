package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PolicyFacts answers the release policy's questions from Gitea, Tekton and
// the release window. Each method fails rather than guessing.
type PolicyFacts struct{ s *Service }

// PolicyFacts returns the read-only fact source for the release policy.
func (s *Service) PolicyFacts() PolicyFacts { return PolicyFacts{s: s} }

// PolicyFacts returns the handler's fact source for the release policy.
func (h *Handler) PolicyFacts() PolicyFacts { return h.svc.PolicyFacts() }

// ConfigMap reads a ConfigMap in the pipelines namespace; found is false on NotFound.
func (f PolicyFacts) ConfigMap(ctx context.Context, name string) (map[string]string, bool, error) {
	clientset, _, err := f.s.cluster.KubernetesClient()
	if err != nil {
		return nil, false, err
	}
	cm, err := clientset.CoreV1().ConfigMaps(f.s.PipelinesNamespace()).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if cm.Data == nil {
		return map[string]string{}, true, nil
	}
	return cm.Data, true, nil
}

// ciPipelines are the pipelines whose Succeeded run vouches for a commit
// (scripts/release/ci_gate.py CI_PIPELINES).
var ciPipelines = []string{"bifrost-ci-python", "bifrost-ci-frontend", "bifrost-ci-platform"}

// WriteConfigMap replaces the data of a ConfigMap in the pipelines
// namespace, creating it when missing. The release policy's install, freeze
// and unfreeze routes are its only callers; each verifies before writing.
func (f PolicyFacts) WriteConfigMap(ctx context.Context, name string, data map[string]string) error {
	clientset, _, err := f.s.cluster.KubernetesClient()
	if err != nil {
		return err
	}
	cms := clientset.CoreV1().ConfigMaps(f.s.PipelinesNamespace())
	cm, err := cms.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = cms.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{"bifrost.io/purpose": "release-policy"},
			},
			Data: data,
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	cm.Data = data
	_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// giteaRequest GETs an API path under the org; found is false on 404.
func (f PolicyFacts) giteaRequest(ctx context.Context, path string) (*http.Response, bool, error) {
	acc, err := f.s.GiteaAccess(ctx)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, acc.Base+"/api/v1/repos/"+acc.Org+"/"+path, nil)
	if err != nil {
		return nil, false, err
	}
	if acc.User != "" && acc.Pass != "" {
		req.SetBasicAuth(acc.User, acc.Pass)
	}
	resp, err := f.s.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("http: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return nil, false, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, true, nil
}

func (f PolicyFacts) giteaGet(ctx context.Context, path string, out any) (bool, error) {
	resp, found, err := f.giteaRequest(ctx, path)
	if err != nil || !found {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return false, fmt.Errorf("json: %w", err)
	}
	return true, nil
}

// FileAt reads a file's text at ref through the raw endpoint, whole: the
// 1 MiB bound on it went in cleanup wave 1 (2026-11-11).
func (f PolicyFacts) FileAt(ctx context.Context, repo, ref, path string) (string, bool, error) {
	resp, found, err := f.giteaRequest(ctx, url.PathEscape(repo)+"/raw/"+escapePath(path)+"?ref="+url.QueryEscape(ref))
	if err != nil || !found {
		return "", false, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, err
	}
	return string(raw), true, nil
}

// ListDir lists the files (not subdirectories) in dir at ref.
func (f PolicyFacts) ListDir(ctx context.Context, repo, ref, dir string) ([]string, error) {
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	found, err := f.giteaGet(ctx, url.PathEscape(repo)+"/contents/"+escapePath(dir)+"?ref="+url.QueryEscape(ref), &entries)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%s has no %s at %s", repo, dir, ref)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type == "file" {
			out = append(out, e.Name)
		}
	}
	return out, nil
}

func escapePath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// MainHead is the commit main points at.
func (f PolicyFacts) MainHead(ctx context.Context, repo string) (string, error) {
	var b struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	found, err := f.giteaGet(ctx, url.PathEscape(repo)+"/branches/main", &b)
	if err != nil {
		return "", err
	}
	if !found || b.Commit.ID == "" {
		return "", fmt.Errorf("%s has no main branch on the mirror", repo)
	}
	return b.Commit.ID, nil
}

// TagCommit resolves a tag to its commit.
func (f PolicyFacts) TagCommit(ctx context.Context, repo, tag string) (string, bool, error) {
	var t struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	found, err := f.giteaGet(ctx, url.PathEscape(repo)+"/tags/"+url.PathEscape(tag), &t)
	if err != nil || !found {
		return "", false, err
	}
	if t.Commit.SHA == "" {
		return "", false, fmt.Errorf("tag %s in %s has no commit", tag, repo)
	}
	return t.Commit.SHA, true, nil
}

// CommitExists reports whether sha is a commit in repo.
func (f PolicyFacts) CommitExists(ctx context.Context, repo, sha string) (bool, error) {
	var c map[string]any
	return f.giteaGet(ctx, url.PathEscape(repo)+"/git/commits/"+url.PathEscape(sha), &c)
}

// ChangedFiles lists the paths touched by the commits to has and from lacks
// (release_policy_diff.go). It fails when from is not an ancestor of to.
func (f PolicyFacts) ChangedFiles(ctx context.Context, repo, from, to string) ([]string, error) {
	acc, err := f.s.GiteaAccess(ctx)
	if err != nil {
		return nil, err
	}
	return f.s.changedFilesBetween(ctx, repo, from, to, acc.User, acc.Pass)
}

// Succeeded reports whether a ci-* run for repo at sha succeeded. The match
// is ci_gate.py's: repo from the repo param or the bifrost.io/repo label, and
// the revision param equal to sha.
func (f PolicyFacts) Succeeded(ctx context.Context, repo, sha string) (bool, error) {
	status, _, err := f.CIRun(ctx, repo, sha)
	return status == CISucceeded, err
}

// The outcomes CIRun reports.
const (
	CISucceeded = "Succeeded"
	CIFailed    = "Failed"
	CIRunning   = "Running"
)

// CIOutcome is what the ci-* runs of one repo at one commit came to.
type CIOutcome struct {
	Status, Run string
	at          time.Time
	reuseErr    error
}

// CIRun reports the ci-* runs for repo at sha (matched as Succeeded does):
// Succeeded and its run if any succeeded, else the newest run's outcome
// (Failed, or Running while it has no verdict) and name; "" when there is
// no such run. A green whose api-scope result is not full (R5:5.7-1 runs
// only the console's share of the Go tests for a console-only change) is
// not counted at all: it cannot vouch for sha, nor stand as its newest run.
// An invalid tested-by green is Unknown with an error naming the run.
func (f PolicyFacts) CIRun(ctx context.Context, repo, sha string) (status, run string, err error) {
	// With a ledger reader set (SetCIReader), the evidence ledger answers and
	// the runs are not listed; CIIndex stays the reading by runs.
	if f.s.ciReader != nil {
		return f.s.ciReader(ctx, repo, sha)
	}
	idx, err := f.CIIndex(ctx)
	o := idx[repo+"@"+sha]
	if err == nil {
		err = o.reuseErr
	}
	return o.Status, o.Run, err
}

// CIReader answers CIRun from another source than the retained runs: the
// status (CISucceeded, CIFailed, or "" when it knows of no verdict) and the
// run it rests on.
type CIReader func(ctx context.Context, repo, sha string) (status, run string, err error)

// SetCIReader has CIRun, and so the release policy's ci_succeeded, ask r
// instead of listing runs; nil restores the runs. Set while wiring, before
// the service serves.
func (s *Service) SetCIReader(r CIReader) { s.ciReader = r }

// CIIndex lists the ci-* runs once and keeps, for each repo@revision,
// CIRun's outcome: so a reader with many commits to look at lists once.
func (f PolicyFacts) CIIndex(ctx context.Context) (map[string]CIOutcome, error) {
	dyn, err := f.s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	list, err := dyn.Resource(pipelineRunGVR).Namespace(f.s.PipelinesNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: "tekton.dev/pipeline in (" + strings.Join(ciPipelines, ",") + ")",
	})
	if err != nil {
		return nil, err
	}
	idx := map[string]CIOutcome{}
	var platformRuns []unstructured.Unstructured
	for _, item := range list.Items {
		if item.GetLabels()["tekton.dev/pipeline"] == "bifrost-ci-platform" {
			platformRuns = append(platformRuns, item)
		}
	}
	for _, item := range list.Items {
		params := map[string]string{}
		if ps, ok, _ := unstructured.NestedSlice(item.Object, "spec", "params"); ok {
			for _, p := range ps {
				m, _ := p.(map[string]any)
				name, _ := m["name"].(string)
				value, _ := m["value"].(string)
				params[name] = value
			}
		}
		runRepo := params["repo"]
		if runRepo == "" {
			runRepo = item.GetLabels()["bifrost.io/repo"]
		}
		if runRepo == "" || params["revision"] == "" {
			continue
		}
		o := CIOutcome{Status: CIRunning, Run: item.GetName(), at: item.GetCreationTimestamp().Time}
		conds, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		for _, c := range conds {
			m, _ := c.(map[string]any)
			if m["type"] != "Succeeded" {
				continue
			}
			switch m["status"] {
			case "True":
				o.Status = CISucceeded
			case "False":
				o.Status = CIFailed
			}
		}
		if o.Status == CISucceeded && hasTestedBy(item) && (runRepo != "bifrost-platform" || !landingci.ValidReuse(item, platformRuns, params["revision"])) {
			o.Status = "Unknown"
			o.reuseErr = fmt.Errorf("CI run %s has invalid tested-by reuse evidence", o.Run)
		}
		if o.Status == CISucceeded && !fullAPIScope(item) {
			continue
		}
		key := runRepo + "@" + params["revision"]
		prev, seen := idx[key]
		switch {
		case !seen, o.Status == CISucceeded && prev.Status != CISucceeded:
			idx[key] = o
		case prev.Status != CISucceeded && o.at.After(prev.at):
			idx[key] = o
		}
	}
	return idx, nil
}

// hasTestedBy: the run claims a reuse (landingci.ClaimsReuse), a malformed
// one included, so it needs verification; an empty tested-by is an ordinary
// run (TD-533).
func hasTestedBy(item unstructured.Unstructured) bool {
	return landingci.ClaimsReuse(item)
}

// fullAPIScope: the run's api-scope result is absent (it predates the result
// and ran everything) or exactly "full". A duplicated or unknown value is not.
func fullAPIScope(item unstructured.Unstructured) bool {
	results, _, _ := unstructured.NestedSlice(item.Object, "status", "results")
	n, scope := 0, ""
	for _, r := range results {
		m, _ := r.(map[string]any)
		if m["name"] == "api-scope" {
			n++
			scope, _ = m["value"].(string)
		}
	}
	return n == 0 || n == 1 && scope == "full"
}

// HeldBy is "" only when a live window covers pipeline and its holder is who.
// It does not share the start gate's rule (decideReleaseWindow), which lets a
// pipeline that need not hold a window run when none is open: here no window
// is a refusal, and so is the expired record every release leaves behind.
func (f PolicyFacts) HeldBy(ctx context.Context, pipeline, who string) string {
	found, window, err := f.s.readReleaseWindow(ctx)
	if err != nil {
		return "cannot read the release window: " + err.Error()
	}
	holder, _ := window["who"].(string)
	holder = strings.TrimSpace(holder)
	if !found || holder == "" || releaseWindowExpired(window, time.Now().UTC()) {
		return "no release window is open; hold one first: release.sh hold --what <repo>"
	}
	who = strings.TrimSpace(who)
	if who == "" {
		return "the request has no who, so it cannot be the window holder"
	}
	what, _ := window["what"].(string)
	held := fmt.Sprintf("who=%s what=%s expires=%v", holder, what, window["expires_at"])
	repos, known := requiredRepos(pipeline)
	if !known || !intersects(parseWhat(what), repos) {
		need := "no release window covers it"
		if known {
			need = "it needs one of " + sortedKeys(repos)
		}
		return fmt.Sprintf("the open release window (%s) does not cover %s: %s", held, pipeline, need)
	}
	if who != holder {
		return fmt.Sprintf("release window held by someone else (%s)", held)
	}
	return ""
}
