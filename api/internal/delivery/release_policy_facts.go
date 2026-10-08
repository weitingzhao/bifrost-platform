package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

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

func (f PolicyFacts) giteaGet(ctx context.Context, path string, out any) (bool, error) {
	acc, err := f.s.GiteaAccess(ctx)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, acc.Base+"/api/v1/repos/"+acc.Org+"/"+path, nil)
	if err != nil {
		return false, err
	}
	if acc.User != "" && acc.Pass != "" {
		req.SetBasicAuth(acc.User, acc.Pass)
	}
	resp, err := f.s.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return false, fmt.Errorf("json: %w", err)
	}
	return true, nil
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

// ChangedFiles lists the paths that differ between two commits.
func (f PolicyFacts) ChangedFiles(ctx context.Context, repo, from, to string) ([]string, error) {
	acc, err := f.s.GiteaAccess(ctx)
	if err != nil {
		return nil, err
	}
	return f.s.fetchGiteaCompareFiles(ctx, repo, from, to, acc.User, acc.Pass)
}

// Succeeded reports whether a ci-* run for repo at sha succeeded. The match
// is ci_gate.py's: repo from the repo param or the bifrost.io/repo label, and
// the revision param equal to sha.
func (f PolicyFacts) Succeeded(ctx context.Context, repo, sha string) (bool, error) {
	dyn, err := f.s.buildDynamicClient()
	if err != nil {
		return false, err
	}
	list, err := dyn.Resource(pipelineRunGVR).Namespace(f.s.PipelinesNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: "tekton.dev/pipeline in (" + strings.Join(ciPipelines, ",") + ")",
	})
	if err != nil {
		return false, err
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
		if runRepo != repo || params["revision"] != sha {
			continue
		}
		conds, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		for _, c := range conds {
			m, _ := c.(map[string]any)
			if m["type"] == "Succeeded" && m["status"] == "True" {
				return true, nil
			}
		}
	}
	return false, nil
}

// HeldBy is "" when who holds an open window that covers pipeline. Unlike the
// start gate, no window at all is a refusal here.
func (f PolicyFacts) HeldBy(ctx context.Context, pipeline, who string) string {
	found, window, err := f.s.readReleaseWindow(ctx)
	if err != nil {
		return "cannot read the release window: " + err.Error()
	}
	if !found {
		return "no release window is open; hold one first: release.sh hold --what <repo>"
	}
	if strings.TrimSpace(who) == "" {
		return "the request has no who, so it cannot be the window holder"
	}
	return decideReleaseWindow(found, window, pipeline, who)
}
