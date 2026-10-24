package delivery

import (
	"context"
	"encoding/base64"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const (
	readerUserFixture  = "bifrost-platform-reader-stg"
	readerTokenFixture = "0123456789abcdef0123456789abcdef01234567"
	adminTokenFixture  = "fedcba9876543210fedcba9876543210fedcba98"
)

// writeReader makes a GITEA_READER_DIR as the kubelet mounts a basic-auth
// Secret: one file per key, the value as written (here with a newline).
func writeReader(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, v := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGiteaReaderFromEnv(t *testing.T) {
	ctx := context.Background()
	t.Setenv(giteaReaderDirEnv, "")
	if _, _, err := giteaReaderFromEnv(ctx); err != errNoGiteaReader {
		t.Fatalf("unset: err = %v", err)
	}

	t.Setenv(giteaReaderDirEnv, writeReader(t, map[string]string{"username": readerUserFixture + "\n", "password": readerTokenFixture + "\n"}))
	user, pass, err := giteaReaderFromEnv(ctx)
	if err != nil || user != readerUserFixture || pass != readerTokenFixture {
		t.Fatalf("mounted: %q %q %v", user, pass, err)
	}

	bad := map[string]struct {
		files map[string]string
		words string
	}{
		"no password":       {map[string]string{"username": readerUserFixture}, "password is missing"},
		"no username":       {map[string]string{"password": readerTokenFixture}, "username is missing"},
		"empty password":    {map[string]string{"username": readerUserFixture, "password": " \n"}, "password is empty"},
		"two lines":         {map[string]string{"username": readerUserFixture, "password": readerTokenFixture + "\n" + readerTokenFixture}, "more than one line"},
		"colon in username": {map[string]string{"username": "a:b", "password": readerTokenFixture}, "holds a ':'"},
		"oversized":         {map[string]string{"username": readerUserFixture, "password": strings.Repeat("a", maxReaderFile+1)}, "longer than"},
	}
	for name, tc := range bad {
		t.Run(name, func(t *testing.T) {
			t.Setenv(giteaReaderDirEnv, writeReader(t, tc.files))
			user, pass, err := giteaReaderFromEnv(ctx)
			if err == nil || user != "" || pass != "" {
				t.Fatalf("accepted: %q %q %v", user, pass, err)
			}
			if !strings.Contains(err.Error(), tc.words) {
				t.Fatalf("err = %v, want %q", err, tc.words)
			}
			if strings.Contains(err.Error(), readerTokenFixture) {
				t.Fatalf("error carries the token: %v", err)
			}
		})
	}
}

func TestNewHandlerReadsTheReader(t *testing.T) {
	h := NewHandler(&config.Config{}, nil)
	if h.svc.readerCreds == nil {
		t.Fatal("the server's delivery service has no Gitea reader")
	}
}

// fakeGitea answers every Gitea read the delivery service makes and records
// the Authorization header of each request.
type fakeGitea struct {
	mu    sync.Mutex
	auths []string
	srv   *httptest.Server
}

func newFakeGitea(t *testing.T) *fakeGitea {
	t.Helper()
	g := &fakeGitea{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.auths = append(g.auths, r.Header.Get("Authorization"))
		g.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches"):
			_, _ = w.Write([]byte(`[{"name":"main","commit":{"id":"` + factSHA1 + `"}}]`))
		case strings.HasSuffix(r.URL.Path, "/tags"):
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/git/commits/"):
			_, _ = w.Write([]byte(`{"sha":"` + factSHA1 + `"}`))
		case strings.Contains(r.URL.Path, "/compare/"):
			_, _ = w.Write([]byte(`{"files":[{"filename":"a.go"}]}`))
		case r.Method == http.MethodGet && strings.Count(strings.TrimPrefix(r.URL.Path, "/api/v1/repos/"), "/") == 1:
			_, _ = w.Write([]byte(`{"mirror":false}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(g.srv.Close)
	t.Setenv("GITEA_BASE", g.srv.URL)
	return g
}

func (g *fakeGitea) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.auths...)
}

// serverService is the delivery service as NewHandler builds it, on a fake
// cluster that still holds the administrator's Secret in cicd as it does
// today (TD-432), and a fake Gitea.
func serverService(t *testing.T) (*Service, *k8sfake.Clientset, *fakeGitea) {
	t.Helper()
	g := newFakeGitea(t)
	cs := k8sfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "gitea-" + "bootstrap", Namespace: "cicd"},
		Data:       map[string][]byte{"gitea_admin" + "_token": []byte(adminTokenFixture), "username": []byte("weitingzhao"), "password": []byte(adminTokenFixture)},
	})
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetClientsetForTest(cs)
	s.readerCreds = giteaReaderFromEnv
	s.giteaBase = g.srv.URL
	s.policy = &actuationpolicy.Policy{Mirrors: actuationpolicy.Mirrors{Repos: []string{"bifrost-platform"}}}
	s.httpClient = &http.Client{Timeout: 2 * time.Second}
	return s, cs, g
}

// readAll makes every kind of Gitea read the delivery service has.
func readAll(t *testing.T, s *Service) (RevisionsResponse, CompareResponse, RefPreflightResponse, error, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rev := s.Revisions(ctx, []string{"bifrost-platform"})
	cmp := s.Compare(ctx, "bifrost-platform", "main", factSHA1)
	pf := s.RefPreflight(ctx, "bifrost-deliver-platform", factSHA1)
	_, accErr := s.GiteaAccess(ctx)
	_, syncErr := s.SyncMirrors(ctx, []string{"bifrost-platform"}, nil)
	_ = s.signedPresent(ctx, []string{"bifrost-platform"}, func(string) string { return factSHA1 })
	return rev, cmp, pf, accErr, syncErr
}

func assertNoSecretRead(t *testing.T, cs *k8sfake.Clientset) {
	t.Helper()
	for _, a := range cs.Actions() {
		if a.GetResource().Resource == "secrets" {
			t.Fatalf("the delivery service asked the API server for a Secret: %v", a)
		}
	}
}

func TestWithoutReaderGiteaIsNotAskedAndNoSecretIsRead(t *testing.T) {
	s, cs, g := serverService(t)
	t.Setenv(giteaReaderDirEnv, "")
	rev, cmp, pf, accErr, syncErr := readAll(t, s)
	if accErr == nil || syncErr == nil {
		t.Fatalf("GiteaAccess err %v, SyncMirrors err %v: want both to say there is no reader", accErr, syncErr)
	}
	for name, detail := range map[string]string{"revisions": rev.Detail, "compare": cmp.Detail, "preflight": pf.Detail} {
		if !strings.Contains(detail, giteaReaderDirEnv) {
			t.Fatalf("%s detail %q does not name %s", name, detail, giteaReaderDirEnv)
		}
	}
	if rev.Reachability != probe.ReachFail || pf.Reachability != probe.ReachFail || pf.Ready || len(pf.Missing) != 0 {
		t.Fatalf("no reader must read as unknown: revisions %s, preflight %+v", rev.Reachability, pf)
	}
	if got := g.seen(); len(got) != 0 {
		t.Fatalf("Gitea was asked without a reader: %v", got)
	}
	if s.SupplyChain(context.Background()).MirrorCredentialsConfigured {
		t.Fatal("supply chain says a Gitea credential is configured")
	}
	assertNoSecretRead(t, cs)
}

func TestReaderIsTheOnlyCredentialSent(t *testing.T) {
	s, cs, g := serverService(t)
	t.Setenv(giteaReaderDirEnv, writeReader(t, map[string]string{"username": readerUserFixture + "\n", "password": readerTokenFixture + "\n"}))
	rev, cmp, pf, accErr, syncErr := readAll(t, s)
	if accErr != nil || syncErr != nil {
		t.Fatalf("GiteaAccess err %v, SyncMirrors err %v", accErr, syncErr)
	}
	if rev.Reachability != probe.ReachOK || cmp.Reachability != probe.ReachOK || pf.Reachability != probe.ReachOK {
		t.Fatalf("reads failed: revisions %s %q, compare %s %q, preflight %s %q",
			rev.Reachability, rev.Detail, cmp.Reachability, cmp.Detail, pf.Reachability, pf.Detail)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(readerUserFixture+":"+readerTokenFixture))
	got := g.seen()
	if len(got) == 0 {
		t.Fatal("Gitea was never asked")
	}
	for _, a := range got {
		if a != want {
			t.Fatalf("a Gitea request carried %q, want the reader's basic auth", a)
		}
	}
	if !s.SupplyChain(context.Background()).MirrorCredentialsConfigured {
		t.Fatal("supply chain does not see the reader")
	}
	assertNoSecretRead(t, cs)
}

// TestNoGiteaAdminTokenInPlatformCode keeps the administrator's Secret and
// key out of every non-test Go file of the module (TD-432 ratchet): the
// platform has its own read-only account and must not learn the other.
func TestNoGiteaAdminTokenInPlatformCode(t *testing.T) {
	root, absErr := filepath.Abs(filepath.Join("..", ".."))
	if absErr != nil {
		t.Fatal(absErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("module root not found at %s: %v", root, statErr)
	}
	banned := []string{"gitea-" + "bootstrap", "gitea_admin" + "_token"}
	scanned := 0
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "vendor" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		for _, b := range banned {
			if strings.Contains(string(raw), b) {
				t.Errorf("%s names %q: the platform reads Gitea with its own reader only (TD-432)", path, b)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if scanned < 50 {
		t.Fatalf("scanned %d Go files under %s; the walk is not seeing the module", scanned, root)
	}
}
