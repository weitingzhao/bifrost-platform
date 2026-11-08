package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAgentdPackageUsesDedicatedReader(t *testing.T) {
	mount := func(user string) string {
		t.Helper()
		dir := t.TempDir()
		for name, value := range map[string]string{"username": user, "password": "fixture-package-read"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	t.Setenv("GITEA_READER_DIR", mount("repository-reader"))
	t.Setenv("AGENTD_PACKAGE_READER_DIR", mount("package-reader"))
	var requests atomic.Int32
	sha := strings.Repeat("b", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		user, pass, ok := r.BasicAuth()
		if !ok || user != "package-reader" || pass != "fixture-package-read" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintln(w, sha+"  agentd-linux-amd64")
	}))
	defer srv.Close()
	t.Setenv(agentdPackageURLEnv, srv.URL+"/{commit}/agentd.sha256")
	got, err := agentdPackage().SHA256(context.Background(), strings.Repeat("a", 40))
	if err != nil || got != sha {
		t.Fatalf("package read failed: %v", err)
	}
	t.Setenv("AGENTD_PACKAGE_READER_DIR", "")
	if _, err := agentdPackage().SHA256(context.Background(), strings.Repeat("a", 40)); err == nil {
		t.Fatal("missing package credential accepted")
	}
	if requests.Load() != 1 {
		t.Fatal("missing credential fell back to an HTTP request")
	}
}
