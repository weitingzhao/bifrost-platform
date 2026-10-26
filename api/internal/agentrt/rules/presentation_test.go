package rules

import "testing"

// CUT §2.3: this test invokes only the pure display predicate, with no rule
// parsing, repository, filesystem, database, or controller.
func TestLandingPresentationOnly(t *testing.T) {
	allow := []string{"console/src/components/**/*.tsx"}
	for _, tc := range []struct {
		name, old, new, added string
		want                  bool
	}{
		{"copy", "console/src/components/A.tsx", "console/src/components/A.tsx", "<p>Ready</p>", true},
		{"new nested component", "", "console/src/components/deep/A.tsx", "import X from './X'", true},
		{"deletion", "console/src/components/A.tsx", "", "", true},
		{"old rename path", "README.md", "console/src/components/A.tsx", "", false},
		{"new rename path", "console/src/components/A.tsx", "README.md", "", false},
		{"case sensitive", "", "Console/src/components/A.tsx", "", false},
		{"root anchored", "", "x/console/src/components/A.tsx", "", false},
		{"traversal", "", "console/src/components/../A.tsx", "", false},
		{"no path", "", "", "", false},
		{"api import", "", "console/src/components/A.tsx", "import X from 'api/x'", false},
		{"lib import", "", "console/src/components/A.tsx", "import X from '@/lib/x'", false},
		{"multiline hooks", "", "console/src/components/A.tsx", "import {\n x\n} from '../hooks/x'", false},
		{"fetch", "", "console/src/components/A.tsx", "fetch ('x')", false},
		{"xhr", "", "console/src/components/A.tsx", "new XMLHttpRequest()", false},
		{"query", "", "console/src/components/A.tsx", "useQuery()", false},
		{"mutation", "", "console/src/components/A.tsx", "useMutation()", false},
		{"location", "", "console/src/components/A.tsx", "window.location.href", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := LandingChange{OldPath: tc.old, NewPath: tc.new, AddedText: tc.added}
			if got := LandingPresentationOnly(allow, []LandingChange{c}); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	if LandingPresentationOnly(allow, nil) {
		t.Fatal("empty change admitted")
	}
}
