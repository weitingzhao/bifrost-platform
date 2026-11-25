package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/uicheck"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
)

// UIPinConfigMap is the stable ui pin, a state ConfigMap of the platform's own
// namespace: the ui commit the console last passed with (agentrt/uicheck). CI
// clones ui there when its uiRevision is "stable" (infra pipeline-ci-platform
// resolve-ui reads it across namespaces). Not in cicd: the admission policy
// bifrost-cicd-configmap-writers keeps every cicd ConfigMap another workload
// reads away from the platform (2026-11-25, the first pin write was denied).
const UIPinConfigMap = "platform-state-ui-stable"

const uiGitea = "http://gitea.cicd.svc.cluster.local:3000"

var uiFullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// wireUICheck gives PROD's controller the ui pin loop (R0E, 2026-11-25):
// checks run with the landing CI's client, the pin is a state ConfigMap of the
// pod's namespace (the platform's state role is bound there), ui is read with
// the platform's read-only Gitea identity. AGENTRT_UI_CHECK=off, another
// environment or no cluster wires none.
func wireUICheck(c *controller.Controller, lent *CILent) {
	if os.Getenv("AGENTRT_UI_CHECK") == "off" || c.Env != "prod" || c.DB == nil || lent == nil || lent.Kubernetes == nil {
		return
	}
	ci := lent.client()
	namespace := k8sstate.Namespace()
	if namespace == "" {
		return
	}
	reader := uiReader{dir: strings.TrimSpace(os.Getenv(landinggit.ReaderDirEnv)), client: &http.Client{Timeout: 20 * time.Second}}
	check := &uicheck.Check{
		UIHead: func(ctx context.Context) (string, error) { return reader.head(ctx, "bifrost-ui") },
		PlatformHead: func(ctx context.Context) (string, error) {
			g, err := landinggit.ReaderFromEnv("bifrost-platform")
			if err != nil {
				return "", err
			}
			return g.Ref(ctx, "refs/heads/main")
		},
		Version: func(ctx context.Context, commit string) (string, error) {
			return reader.version(ctx, "bifrost-ui", commit)
		},
		Pin: func(ctx context.Context) (uicheck.Pin, bool, error) {
			k, err := lent.Kubernetes()
			if err != nil {
				return uicheck.Pin{}, false, err
			}
			cm, err := k.CoreV1().ConfigMaps(namespace).Get(kubemetrics.WithSite(ctx, "uicheck.pin"), UIPinConfigMap, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return uicheck.Pin{}, false, nil
			}
			if err != nil {
				return uicheck.Pin{}, false, err
			}
			return pinOf(cm.Data), true, nil
		},
		SetPin: func(ctx context.Context, p uicheck.Pin) error {
			k, err := lent.Kubernetes()
			if err != nil {
				return err
			}
			cms := k.CoreV1().ConfigMaps(namespace)
			ctx = kubemetrics.WithSite(ctx, "uicheck.pin")
			cm, err := cms.Get(ctx, UIPinConfigMap, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				_, err = cms.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: UIPinConfigMap, Namespace: namespace,
					Labels: map[string]string{"bifrost.io/purpose": "ui-pin"}}, Data: dataOf(p)}, metav1.CreateOptions{})
				return err
			}
			if err != nil {
				return err
			}
			cm.Data = dataOf(p)
			_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
			return err
		},
		Ensure: func(ctx context.Context, platform, ui string) (string, error) {
			return ci.EnsureUICheck(kubemetrics.WithSite(ctx, "landingci.uicheck"), platform, ui)
		},
		Status: func(ctx context.Context, run string) (string, error) {
			s, err := ci.RunStatus(kubemetrics.WithSite(ctx, "landingci.uicheck"), run)
			if errors.Is(err, landingci.ErrRunGone) {
				return "", uicheck.ErrNoRun
			}
			return s, err
		},
		Logf: c.Logf,
		Own:  func(run string) bool { return strings.HasPrefix(run, "ui-check-") },
		Notify: func(ctx context.Context, ui, run, stable string) {
			thread, err := reader.session(ctx, "bifrost-ui", ui)
			if err != nil || thread == "" {
				c.Logf("agentrt-ui-check: nobody told of ui %s kept off (%v)", ui, err)
				return
			}
			body := fmt.Sprintf("bifrost-ui %s is kept off the stable pin: the console's CI with it failed (run %s). "+
				"console and the Trade frontend stay on ui %s until a later ui commit passes. "+
				"Read the run: GET /api/v1/delivery/runs/%s", ui, run, stable, run)
			if lent.Tell == nil {
				return
			}
			if err := lent.Tell(ctx, "thread:"+thread, body); err != nil {
				c.Logf("agentrt-ui-check: telling %s of ui %s failed: %v", thread, ui, err)
			}
		},
	}
	c.Loops = append(c.Loops, controller.Loop{Name: "agentrt-ui-check", Every: uicheck.Every, Run: func(ctx context.Context) error {
		passCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		_, err := check.Pass(passCtx)
		return err
	}})
}

// uiPinCommit reads the stable pin's commit for the full runs (R0E): "" when
// there is no pin, it cannot be read, or the pin loop is off.
func uiPinCommit(lent *CILent) func(ctx context.Context) string {
	if os.Getenv("AGENTRT_UI_CHECK") == "off" || lent == nil || lent.Kubernetes == nil {
		return nil
	}
	return func(ctx context.Context) string {
		namespace := k8sstate.Namespace()
		k, err := lent.Kubernetes()
		if namespace == "" || err != nil {
			return ""
		}
		cm, err := k.CoreV1().ConfigMaps(namespace).Get(kubemetrics.WithSite(ctx, "uicheck.pin"), UIPinConfigMap, metav1.GetOptions{})
		if err != nil {
			return ""
		}
		return cm.Data["commit"]
	}
}

func dataOf(p uicheck.Pin) map[string]string {
	out := map[string]string{}
	for k, v := range map[string]string{"commit": p.Commit, "version": p.Version, "run": p.Run, "at": p.At,
		"blocked_commit": p.BlockedCommit, "blocked_run": p.BlockedRun, "blocked_at": p.BlockedAt} {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

func pinOf(d map[string]string) uicheck.Pin {
	return uicheck.Pin{Commit: d["commit"], Version: d["version"], Run: d["run"], At: d["at"],
		BlockedCommit: d["blocked_commit"], BlockedRun: d["blocked_run"], BlockedAt: d["blocked_at"]}
}

// uiReader reads a repository's main head and a file of a commit through
// Gitea's API with the platform's read-only identity (GITEA_READER_DIR).
type uiReader struct {
	dir    string
	client *http.Client
}

func (r uiReader) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uiGitea+"/api/v1"+path, nil)
	if err != nil {
		return nil, err
	}
	if r.dir != "" {
		user, uerr := os.ReadFile(filepath.Join(r.dir, "username"))
		pass, perr := os.ReadFile(filepath.Join(r.dir, "password"))
		if uerr == nil && perr == nil {
			req.SetBasicAuth(strings.TrimSpace(string(user)), strings.TrimSpace(string(pass)))
		}
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitea %s: status %d", path, resp.StatusCode)
	}
	return body, nil
}

func (r uiReader) head(ctx context.Context, repo string) (string, error) {
	body, err := r.get(ctx, "/repos/bifrost/"+url.PathEscape(repo)+"/branches/main")
	if err != nil {
		return "", err
	}
	var b struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &b); err != nil || !uiFullSHA.MatchString(b.Commit.ID) {
		return "", fmt.Errorf("gitea: no main head of %s", repo)
	}
	return b.Commit.ID, nil
}

var claudeSession = regexp.MustCompile(`(?m)^Claude-Session:\s*(local_[0-9a-f-]{36})\s*$`)

// session is the Claude-Session trailer of a commit's message: the thread that
// made it, "" when it carries none.
func (r uiReader) session(ctx context.Context, repo, commit string) (string, error) {
	body, err := r.get(ctx, "/repos/bifrost/"+url.PathEscape(repo)+"/git/commits/"+url.PathEscape(commit))
	if err != nil {
		return "", err
	}
	var c struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return "", err
	}
	if m := claudeSession.FindStringSubmatch(c.Commit.Message); m != nil {
		return m[1], nil
	}
	return "", nil
}

func (r uiReader) version(ctx context.Context, repo, commit string) (string, error) {
	body, err := r.get(ctx, "/repos/bifrost/"+url.PathEscape(repo)+"/raw/package.json?ref="+url.QueryEscape(commit))
	if err != nil {
		return "", err
	}
	var p struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return "", err
	}
	return p.Version, nil
}
