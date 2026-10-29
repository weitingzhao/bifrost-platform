package rules

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var decisionCall = regexp.MustCompile(`\b(postApprovalDecision|postApprovalResolve|createApprovalRequest|revokeLandingGrant|operatorToken|APPROVAL_TOKEN_STORAGE_KEY)\b|navigator\s*\.\s*serviceWorker`)

// Direct syntax ratchet, not a complete data-flow proof. Aliases, computed
// property names and indirect wrappers still require semantic review.
var authorizationHeader = regexp.MustCompile(`(?i)((?:\.\s*(?:set|append)|\[\s*["'](?:set|append)["']\s*\])\s*\(\s*["']Authorization["']|(?:^|[{,])\s*["']?Authorization["']?\s*:|\[\s*["']Authorization["']\s*\]\s*=|\.\s*Authorization\s*=|\[\s*["']Authorization["']\s*,)`)

func consoleSensitive(source []byte) bool {
	return decisionCall.Match(source) || authorizationHeader.Match(source)
}
func scanConsole(root string, covered func(string) bool) ([]string, error) {
	var missing []string
	err := filepath.WalkDir(filepath.Join(root, "console/src"), func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "__tests__" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(entry.Name(), ".test.") || strings.Contains(entry.Name(), ".spec.") {
			return nil
		}
		switch filepath.Ext(p) {
		case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
		default:
			return nil
		}
		source, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if consoleSensitive(source) && !covered(relative) {
			missing = append(missing, relative)
		}
		return nil
	})
	return missing, err
}
func TestLandingHardConsoleCallSites(t *testing.T) {
	t.Parallel()
	missing, err := scanConsole(repositoryRoot(t), func(p string) bool { return inLandingEntries(p, landingHardConsole[:]) })
	if err != nil || len(missing) > 0 {
		t.Fatalf("unprotected decision/credential call sites: %v: %v", missing, err)
	}
}
func TestLandingHardConsoleDetectsNewCallSite(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"postApprovalDecision(x)", "postApprovalResolve(x)", "createApprovalRequest(x)", "revokeLandingGrant(x)", "operatorToken()", "APPROVAL_TOKEN_STORAGE_KEY", "headers.set('Authorization', token)", "const h = { Authorization: token }", "h['Authorization'] = token", "navigator.serviceWorker.register('/sw.js')"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			file := filepath.Join(dir, "console/src/new.ts")
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if missing, err := scanConsole(dir, func(string) bool { return true }); err != nil || len(missing) != 0 {
				t.Fatal("control", missing, err)
			}
			if missing, err := scanConsole(dir, func(string) bool { return false }); err != nil || len(missing) != 1 {
				t.Fatal("scan missed", missing, err)
			}
		})
	}
}
