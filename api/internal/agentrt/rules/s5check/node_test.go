package s5check

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNodeCheckerIsolatedProtocolAndDigest(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "checker.cjs")
	source := []byte(`if(process.env.NODE_OPTIONS||process.env.CANDIDATE_SECRET)process.exit(9);let s='';process.stdin.on('data',c=>s+=c);process.stdin.on('end',()=>{let r=JSON.parse(s);if(r.schema!==2)process.exit(7);process.stdout.write(JSON.stringify({findings:[],policyDigest:'a'.repeat(64),callersDigest:'c'.repeat(64),typesDigest:'d'.repeat(64),testsDigest:'e'.repeat(64)}))});`)
	if err := os.WriteFile(script, source, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NODE_OPTIONS", "--require=/candidate/evil.cjs")
	t.Setenv("CANDIDATE_SECRET", "must-not-reach-checker")
	n := NodeChecker{NodeBinary: node, Script: script, ScriptDigest: SHA256(source), ModulePath: dir, Timeout: 5 * time.Second}
	result, err := n.Check(context.Background(), StaticRequest{Schema: 2, Policies: json.RawMessage(`{}`), Result: TreeSnapshot{Files: map[string]TreeEntry{"evil.ts": {Content: "process.exit(99)", Mode: "100644"}}}})
	if err != nil || len(result.Findings) != 0 {
		t.Fatalf("fixed protocol: %+v %v", result, err)
	}
	if err := os.WriteFile(script, []byte("throw new Error('changed')"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = n.Check(context.Background(), StaticRequest{}); err == nil {
		t.Fatal("changed trusted checker was executed")
	}
}

func TestNodeCheckerRejectsForgedOrMissingResult(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed")
	}
	for _, body := range []string{`console.log('{}')`, `console.log('{"findings":[]}')`, `console.log('{"findings":[],"passed":true}')`, `console.log('{"findings":[]}');console.log('{}')`, `setInterval(()=>{},1000)`} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			script := filepath.Join(dir, "checker.cjs")
			source := []byte(body)
			if err := os.WriteFile(script, source, 0600); err != nil {
				t.Fatal(err)
			}
			n := NodeChecker{NodeBinary: node, Script: script, ScriptDigest: SHA256(source), ModulePath: dir, Timeout: 250 * time.Millisecond}
			if _, err = n.Check(context.Background(), StaticRequest{Schema: 2, Policies: json.RawMessage(`{}`)}); err == nil {
				t.Fatal("invalid checker output accepted")
			}
		})
	}
}
