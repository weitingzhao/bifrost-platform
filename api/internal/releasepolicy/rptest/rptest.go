// Package rptest builds signed release policies and fake fact sources for
// tests. Keys are generated per test in memory; none is the Owner's.
package rptest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// Key is a throwaway signing key and its SHA256 fingerprint.
type Key struct {
	Signer      ssh.Signer
	Fingerprint string
}

// NewKey generates an ed25519 key for one test.
func NewKey(t testing.TB) Key {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return Key{Signer: signer, Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}
}

// Sign returns an armored SSHSIG like `ssh-keygen -Y sign -n namespace`.
func (k Key) Sign(t testing.TB, message []byte, namespace string) string {
	t.Helper()
	h := sha512.Sum512(message)
	signed := append([]byte("SSHSIG"), ssh.Marshal(struct {
		Namespace, Reserved, HashAlgorithm string
		Hash                               []byte
	}{namespace, "", "sha512", h[:]})...)
	sig, err := k.Signer.Sign(rand.Reader, signed)
	if err != nil {
		t.Fatal(err)
	}
	blob := append([]byte("SSHSIG"), ssh.Marshal(struct {
		Version                            uint32
		PublicKey                          []byte
		Namespace, Reserved, HashAlgorithm string
		Signature                          []byte
	}{1, k.Signer.PublicKey().Marshal(), namespace, "", "sha512", ssh.Marshal(sig)})...)
	return string(pem.EncodeToMemory(&pem.Block{Type: "SSH SIGNATURE", Bytes: blob}))
}

// AllowedSigners is the allowed_signers line for this key.
func (k Key) AllowedSigners() string {
	return `owner namespaces="bifrost-release-policy,bifrost-release-unfreeze" ` + string(ssh.MarshalAuthorizedKey(k.Signer.PublicKey()))
}

// PolicyText renders a policy the way release.sh policy sign does
// (canonical JSON), with invented path rules.
func PolicyText(id string, signedAt time.Time, days int, allow ...string) string {
	doc := map[string]any{
		"version":    1,
		"policy_id":  id,
		"signed_by":  "owner",
		"signed_at":  signedAt.UTC().Format(time.RFC3339),
		"expires_at": signedAt.Add(time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339),
		"valid_days": days,
		"allow":      allow,
		"conditions": map[string]any{
			"revision": "main-or-tag", "ci_succeeded": true, "window_held_by_requester": true,
			"no_pending_before_db_steps": true, "no_ddl": true, "additive_ddl": true,
			"no_d10_paths": true, "no_trust_anchor_change": true,
		},
		"limits":    map[string]any{"rolling_24h": "none", "concurrency": 1},
		"reminders": map[string]any{"on_blocked_after_expiry": true},
		"ci_repos":  []string{"repo-app", "repo-lib"},
		"db_steps": map[string]any{
			"repo": "repo-ops", "dir": "db-steps.d",
			"pipelines": map[string]string{"deliver-with-db": "prod"},
		},
		"pinned": map[string]any{
			"deliver-pinned": map[string]any{
				"from":   "deliver-app-stg",
				"params": map[string]string{"appRevision": "repo-app", "revision": "repo-lib"},
			},
		},
		"paths": map[string]any{
			"no_ddl": []map[string]any{
				{"repo": "*", "paths": []string{"**/ddl*.py", "**/*.sql", "**/migrations/**"}},
			},
			"no_d10_paths": []map[string]any{
				{"repo": "repo-app", "paths": []string{"engine/orders/**"}},
			},
			"no_trust_anchor_change": []map[string]any{
				{"repo": "repo-app", "paths": []string{"internal/releasepolicy/**"}},
			},
		},
	}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	return string(raw) + "\n"
}

// ConfigMaps is an in-memory ConfigMap reader. Err makes every read fail.
// Each Set or successful write bumps a resourceVersion, and a write whose
// version is not the one just read returns ErrConflict and leaves the object.
type ConfigMaps struct {
	mu   sync.Mutex
	Data map[string]map[string]string
	rv   map[string]uint64
	Err  map[string]error
}

// NewConfigMaps returns an empty reader.
func NewConfigMaps() *ConfigMaps {
	return &ConfigMaps{
		Data: map[string]map[string]string{},
		rv:   map[string]uint64{},
		Err:  map[string]error{},
	}
}

// Set stores one ConfigMap; nil deletes it. A store bumps resourceVersion.
func (c *ConfigMaps) Set(name string, data map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setLocked(name, data)
}

func (c *ConfigMaps) setLocked(name string, data map[string]string) {
	if c.Data == nil {
		c.Data = map[string]map[string]string{}
	}
	if c.rv == nil {
		c.rv = map[string]uint64{}
	}
	if data == nil {
		delete(c.Data, name)
		delete(c.rv, name)
		return
	}
	c.Data[name] = cloneMap(data)
	c.rv[name]++
}

func (c *ConfigMaps) ConfigMap(_ context.Context, name string) (map[string]string, string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Err[name]; err != nil {
		return nil, "", false, err
	}
	d, ok := c.Data[name]
	if !ok {
		return nil, "", false, nil
	}
	return cloneMap(d), strconv.FormatUint(c.rv[name], 10), true, nil
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Facts fakes Git, CI, Window and Deployed.
type Facts struct {
	Heads    map[string]string
	Tags     map[string]map[string]string
	Commits  map[string]bool // "repo@sha"
	Files    map[string][]string
	FilesErr error
	Text     map[string]string   // "repo@ref:path" -> file text
	Dirs     map[string][]string // "repo@ref:dir" -> file names
	Green    map[string]bool     // "repo@sha"
	Window   string
	Records  map[string]map[string]string
	Missing  []string
}

func (f *Facts) FileAt(_ context.Context, repo, ref, path string) (string, bool, error) {
	text, ok := f.Text[repo+"@"+ref+":"+path]
	return text, ok, nil
}

func (f *Facts) ListDir(_ context.Context, repo, ref, dir string) ([]string, error) {
	names, ok := f.Dirs[repo+"@"+ref+":"+dir]
	if !ok {
		return nil, fmt.Errorf("%s has no %s at %s", repo, dir, ref)
	}
	return names, nil
}

// Writer is an in-memory ConfigMapWriter over a ConfigMaps reader.
type Writer struct{ CMs *ConfigMaps }

// WriteConfigMap updates name only when resourceVersion is the version
// ConfigMap last returned. An empty version creates the object; if it
// already exists the write conflicts and changes nothing.
func (w Writer) WriteConfigMap(_ context.Context, name string, data map[string]string, resourceVersion string) error {
	w.CMs.mu.Lock()
	defer w.CMs.mu.Unlock()
	if w.CMs.Data == nil {
		w.CMs.Data = map[string]map[string]string{}
	}
	if w.CMs.rv == nil {
		w.CMs.rv = map[string]uint64{}
	}
	_, ok := w.CMs.Data[name]
	if !ok {
		if resourceVersion != "" {
			return releasepolicy.ErrConflict
		}
		w.CMs.setLocked(name, data)
		return nil
	}
	if resourceVersion != strconv.FormatUint(w.CMs.rv[name], 10) {
		return releasepolicy.ErrConflict
	}
	w.CMs.setLocked(name, data)
	return nil
}

func (f *Facts) MainHead(_ context.Context, repo string) (string, error) {
	if h, ok := f.Heads[repo]; ok {
		return h, nil
	}
	return "", fmt.Errorf("%s has no main", repo)
}

func (f *Facts) TagCommit(_ context.Context, repo, tag string) (string, bool, error) {
	sha, ok := f.Tags[repo][tag]
	return sha, ok, nil
}

func (f *Facts) CommitExists(_ context.Context, repo, sha string) (bool, error) {
	return f.Commits[repo+"@"+sha] || f.Heads[repo] == sha, nil
}

func (f *Facts) ChangedFiles(_ context.Context, repo, from, to string) ([]string, error) {
	if f.FilesErr != nil {
		return nil, f.FilesErr
	}
	return f.Files[repo], nil
}

func (f *Facts) Succeeded(_ context.Context, repo, sha string) (bool, error) {
	return f.Green[repo+"@"+sha], nil
}

func (f *Facts) HeldBy(context.Context, string, string) string { return f.Window }

func (f *Facts) Deployed(_ context.Context, pipeline string) (map[string]string, []string, error) {
	return f.Records[pipeline], f.Missing, nil
}

// SHA builds a 40-character hex commit id from a short seed.
func SHA(seed string) string {
	out := []byte(fmt.Sprintf("%x", seed))
	for len(out) < 40 {
		out = append(out, '0')
	}
	return string(out[:40])
}
