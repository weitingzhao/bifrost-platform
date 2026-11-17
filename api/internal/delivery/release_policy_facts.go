package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// TreeChangedFiles is the complete difference of the two commits' trees
// (release_policy_tree.go), for a diff whose commit lists may be cut short.
func (f PolicyFacts) TreeChangedFiles(ctx context.Context, repo, from, to string) ([]string, error) {
	acc, err := f.s.GiteaAccess(ctx)
	if err != nil {
		return nil, err
	}
	return f.s.treeFilesBetween(ctx, repo, from, to, acc.User, acc.Pass)
}

// Succeeded reports whether CI passed for repo at sha: CIRun's answer is
// CISucceeded. An error, or no verdict, is never a pass.
func (f PolicyFacts) Succeeded(ctx context.Context, repo, sha string) (bool, error) {
	status, _, err := f.CIRun(ctx, repo, sha)
	return err == nil && status == CISucceeded, err
}

// The verdicts CIRun reports.
const (
	CISucceeded = "Succeeded"
	CIFailed    = "Failed"
)

// ErrNoCIReader is CIRun's answer in a process whose CI reader was never set.
var ErrNoCIReader = errors.New("no CI reader is set: the evidence ledger is not wired into this process, so the commit has no verdict")

// CIRun is the CI verdict of repo at sha and the run it rests on, as the
// reader set by SetCIReader answers (the evidence ledger; the server's
// wireEvidenceGate): CISucceeded, CIFailed, or "" when it knows of none.
// There is no other source: with no reader set the answer is no verdict and
// ErrNoCIReader, and an error of the reader is no verdict either. (Until
// 2026-11-16 the retained ci-* runs answered when no reader was set; that
// reading did not count an incremental pass and is retired.)
func (f PolicyFacts) CIRun(ctx context.Context, repo, sha string) (status, run string, err error) {
	if f.s.ciReader == nil {
		return "", "", ErrNoCIReader
	}
	if status, run, err = f.s.ciReader(ctx, repo, sha); err != nil {
		return "", "", err
	}
	return status, run, nil
}

// CIReader answers CIRun: the status (CISucceeded, CIFailed, or "" when it
// knows of no verdict) and the run it rests on.
type CIReader func(ctx context.Context, repo, sha string) (status, run string, err error)

// SetCIReader sets the one source CIRun, and so the release policy's
// ci_succeeded, asks. Set while wiring, before the service serves.
func (s *Service) SetCIReader(r CIReader) { s.ciReader = r }

// WindowHolder names who holds the release window now and what for; open is
// false when no live window exists. It reads and nothing else: no refusal is
// recorded, as a caller that only wants to know must not leave one.
func (f PolicyFacts) WindowHolder(ctx context.Context) (who, what string, open bool, err error) {
	found, window, err := f.s.readReleaseWindow(ctx)
	if err != nil {
		return "", "", false, err
	}
	who, _ = window["who"].(string)
	who = strings.TrimSpace(who)
	if !found || who == "" || releaseWindowExpired(window, time.Now().UTC()) {
		return "", "", false, nil
	}
	what, _ = window["what"].(string)
	return who, what, true, nil
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
