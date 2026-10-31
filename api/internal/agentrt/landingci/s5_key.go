package landingci

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// S5CIKey is the complete, persisted preparation key (S5 §5.1). Its inputs
// come from trusted Git, policy and review readers, never candidate metadata.
// The operation owner allocates Generation under its lock before EnsureS5.
//
// It is an alias of landing.S5CIKey: the key line B persists, its canonical
// bytes, keyDigest and run name are the same values this transport sends to
// Tekton and reads back. There is no second encoding to convert between.
type S5CIKey = landing.S5CIKey

var s5Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
var s5ID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,199}$`)

func nonzeroHex(s string, size int) bool {
	_, err := hex.DecodeString(s)
	return err == nil && len(s) == size && s == strings.ToLower(s) && s != strings.Repeat("0", size)
}

// validateS5Key applies the landing key contract (closed env/tenant, ULID
// task/attempt, op bound to task, generation <= MaxInt64) AND the transport's
// own stricter checks (no all-zero identity, no UNBUILT image). Both must hold.
func validateS5Key(k S5CIKey) error {
	if !k.Valid() {
		return fmt.Errorf("S5_BINDING: invalid landing key")
	}
	if k.Schema != 2 || k.Repo != "bifrost-platform" || k.Generation == 0 || k.RuleVersion <= 0 {
		return fmt.Errorf("S5_BINDING: invalid key version, repo or generation")
	}
	for _, id := range []string{k.Env, k.Tenant, k.Task, k.Attempt, k.Op} {
		if !s5ID.MatchString(id) {
			return fmt.Errorf("S5_BINDING: invalid operation identity")
		}
	}
	for _, sha := range []string{k.Base, k.A, k.M0, k.R, k.BaselineTree, k.ResultTree, k.UISHA} {
		if !fullSHA.MatchString(sha) || !nonzeroHex(sha, 40) {
			return fmt.Errorf("S5_BINDING: immutable Git identity required")
		}
	}
	for _, digest := range []string{k.RuleDigest, k.ProfileDigest, k.ToolchainDigest, k.CallersDigest, k.TypesDigest, k.TestsDigest, k.ReviewBindingDigest, k.PatchEvidenceDigest} {
		if !s5Hex.MatchString(digest) || !nonzeroHex(digest, 64) {
			return fmt.Errorf("S5_BINDING: invalid digest")
		}
	}
	if !strings.HasPrefix(k.CheckerImageDigest, "sha256:") || !nonzeroHex(strings.TrimPrefix(k.CheckerImageDigest, "sha256:"), 64) {
		return fmt.Errorf("S5_CI_IDENTITY: unbuilt checker image")
	}
	return nil
}

// canonicalS5 keeps integers exact, sorts object keys recursively, preserves
// arrays and disables HTML escaping. It encodes CI-side values (evidence,
// resolved specs). The KEY is never encoded here: it always uses
// S5CIKey.Canonical so keyDigest has one algorithm.
func canonicalS5(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var obj any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err = d.Decode(&obj); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err = e.Encode(obj); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

func s5Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// s5Params are the run parameters. "key" is exactly the bytes whose SHA-256
// is keyDigest; callers validate the key first.
func s5Params(k S5CIKey) map[string]string {
	b, _ := k.Canonical()
	d, _ := k.Digest()
	return map[string]string{"base": k.M0, "revision": k.R, "uiRevision": k.UISHA,
		"key": string(b), "keyDigest": d, "generation": strconv.FormatUint(k.Generation, 10)}
}

func decodeS5(b []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	// Duplicate keys and noncanonical numbers/encoding cannot silently collapse.
	c, err := canonicalS5(target)
	if err != nil || !bytes.Equal(b, c) {
		return fmt.Errorf("noncanonical JSON")
	}
	return nil
}
