package s5check

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The runtime checker entrypoints compiled into this binary. A CI installation
// whose policy root executes different JavaScript than the reviewed checker
// revision T this binary was built from is refused before any check runs.
var (
	//go:embed ts/check.cjs
	embeddedCheckJS []byte
	//go:embed ts/compiler.cjs
	embeddedCompilerJS []byte
	//go:embed ts/contracts.cjs
	embeddedContractsJS []byte
	//go:embed ts/analyze.cjs
	embeddedAnalyzeJS []byte
)

// Materials is the pinned checker material emitted by `s5check --ci-materials`
// and recorded in the platform trust root next to the image digest. Every
// field is derived from protected installation files, never from R.
type Materials struct {
	ProfileDigest   string `json:"profileDigest"`
	ToolchainDigest string `json:"toolchainDigest"`
	CallersDigest   string `json:"callersDigest"`
	TypesDigest     string `json:"typesDigest"`
	TestsDigest     string `json:"testsDigest"`
	UISHA           string `json:"uiSHA"`
}

// UIRevisionFile pins the @bifrost/ui revision whose declarations the reviewed
// manifest's toolchain hashes were enrolled against. It is protected profile
// material in the same hard subtree as the manifest.
const UIRevisionFile = "policies/ui-revision.json"

var lowerHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ToolchainDigest is THE S5 toolchainDigest: SHA-256 of the canonical JSON of
// the checker image's TOOLS.lock.json — the same lock whose archives the image
// build verifies (infra docker/s5-checker/verify_materials.py). TypeScript
// configuration and every declaration the compiler host reads are bound by the
// profile (manifest.configs / manifest.toolchain → profileDigest) and are
// re-measured by the checker on every run, so they are not repeated here.
func ToolchainDigest(lock []byte) (string, error) {
	if _, err := toolsLockTypeScript(lock); err != nil {
		return "", err
	}
	return Digest(json.RawMessage(lock))
}

func toolsLockTypeScript(lock []byte) (string, error) {
	var parsed struct {
		TypeScript *struct {
			Version string `json:"version"`
		} `json:"typescript"`
	}
	if len(bytes.TrimSpace(lock)) == 0 {
		return "", errors.New("trusted tool lock is missing")
	}
	if err := json.Unmarshal(lock, &parsed); err != nil {
		return "", fmt.Errorf("invalid trusted tool lock: %w", err)
	}
	if parsed.TypeScript == nil || parsed.TypeScript.Version == "" {
		return "", errors.New("trusted tool lock does not pin TypeScript")
	}
	return parsed.TypeScript.Version, nil
}

// manifestTypeScript returns the compiler version the profile was enrolled with.
func manifestTypeScript(manifest []byte) (string, error) {
	var m struct {
		TypeScript string `json:"typescript"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil || m.TypeScript == "" {
		return "", errors.New("protected policy does not record its TypeScript version")
	}
	return m.TypeScript, nil
}

// toolchainBinding computes the lock digest and requires that the lock pins the
// same TypeScript release the manifest was enrolled with.
func toolchainBinding(lock, manifest []byte) (string, error) {
	digest, err := ToolchainDigest(lock)
	if err != nil {
		return "", err
	}
	locked, _ := toolsLockTypeScript(lock)
	enrolled, err := manifestTypeScript(manifest)
	if err != nil {
		return "", err
	}
	if locked != enrolled {
		return "", fmt.Errorf("tool lock TypeScript %q differs from enrolled profile %q", locked, enrolled)
	}
	return digest, nil
}

// contractDigests mirrors ts/contracts.cjs contractDigests exactly (callers,
// types, tests). Missing object members are omitted and missing array members
// become null, as the JavaScript canonical encoder does with undefined.
func contractDigests(manifest []byte) (callers, types, tests string, err error) {
	var m map[string]json.RawMessage
	if err = json.Unmarshal(manifest, &m); err != nil {
		return "", "", "", fmt.Errorf("invalid protected policy: %w", err)
	}
	var files []map[string]json.RawMessage
	if err = json.Unmarshal(m["files"], &files); err != nil || len(files) == 0 {
		return "", "", "", errors.New("protected policy has no files")
	}
	object := func(src map[string]json.RawMessage, keys ...[2]string) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for _, k := range keys {
			if v, ok := src[k[1]]; ok {
				out[k[0]] = v
			}
		}
		return out
	}
	column := func(name string) []json.RawMessage {
		out := make([]json.RawMessage, len(files))
		for i, f := range files {
			if v, ok := f[name]; ok {
				out[i] = v
			} else {
				out[i] = json.RawMessage("null")
			}
		}
		return out
	}
	callerValue := object(m, [2]string{"relations", "relations"}, [2]string{"dataSources", "dataSources"}, [2]string{"assets", "assets"},
		[2]string{"outsideConsoleSources", "outsideConsoleSources"}, [2]string{"dataContracts", "dataContracts"}, [2]string{"derivedConsumers", "derivedConsumers"})
	callersRaw, _ := json.Marshal(column("callers"))
	selectorsRaw, _ := json.Marshal(column("selectorReferences"))
	callerValue["callers"], callerValue["selectors"] = callersRaw, selectorsRaw
	typeValue := make([]map[string]json.RawMessage, len(files))
	for i, f := range files {
		typeValue[i] = object(f, [2]string{"exports", "exports"}, [2]string{"exportShapes", "exportShapes"}, [2]string{"typeImports", "typeImports"})
	}
	testValue := object(m, [2]string{"originalTests", "trustedTests"}, [2]string{"suite", "trustedSuite"})
	if callers, err = Digest(callerValue); err != nil {
		return "", "", "", err
	}
	if types, err = Digest(typeValue); err != nil {
		return "", "", "", err
	}
	tests, err = Digest(testValue)
	return callers, types, tests, err
}

// regularFile reads a protected installation file below root, refusing
// symlinks on every path component.
func regularFile(root, rel string) ([]byte, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !validPath(rel) {
		return nil, fmt.Errorf("invalid protected path %q", rel)
	}
	current := root
	for _, part := range strings.Split(rel, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("protected path %q is a symlink", rel)
		}
	}
	info, err := os.Lstat(current)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("protected path %q is not a regular file", rel)
	}
	return os.ReadFile(current)
}

// verifyInstallation proves the policy root is the reviewed checker revision
// this binary was built from: the manifest and every executed checker script
// are byte-equal to the compiled-in copies, and every trusted suite file
// matches the manifest's recorded hash. It returns the manifest bytes.
func verifyInstallation(root string) ([]byte, error) {
	manifest, err := regularFile(root, "policies/manifest.json")
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(manifest, embeddedManifest) {
		return nil, errors.New("installed policy manifest is not the reviewed manifest")
	}
	for name, want := range map[string][]byte{"ts/check.cjs": embeddedCheckJS, "ts/compiler.cjs": embeddedCompilerJS, "ts/contracts.cjs": embeddedContractsJS, "ts/analyze.cjs": embeddedAnalyzeJS} {
		got, err := regularFile(root, name)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(got, want) {
			return nil, fmt.Errorf("installed checker %s is not the reviewed checker", name)
		}
	}
	var m struct {
		TrustedSuite []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"trustedSuite"`
	}
	if err = json.Unmarshal(manifest, &m); err != nil || len(m.TrustedSuite) == 0 {
		return nil, errors.New("protected policy has no trusted suite")
	}
	seenRunner := false
	for _, f := range m.TrustedSuite {
		got, err := regularFile(root, f.Path)
		if err != nil {
			return nil, err
		}
		if !lowerHex64.MatchString(f.SHA256) || SHA256(got) != f.SHA256 {
			return nil, fmt.Errorf("trusted suite file %s differs from the manifest", f.Path)
		}
		seenRunner = seenRunner || f.Path == behaviorScript
	}
	if !seenRunner {
		return nil, errors.New("trusted suite does not bind the behavior runner")
	}
	return manifest, nil
}

func pinnedUI(root string) (string, error) {
	raw, err := regularFile(root, UIRevisionFile)
	if err != nil {
		return "", err
	}
	var pin struct {
		UISHA string `json:"uiSHA"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&pin); err != nil {
		return "", fmt.Errorf("invalid UI pin: %w", err)
	}
	if !fullSHA.MatchString(pin.UISHA) || pin.UISHA == strings.Repeat("0", 40) {
		return "", errors.New("UI revision is not pinned")
	}
	return pin.UISHA, nil
}

// ComputeMaterials derives the pinned material of a protected installation.
// UNBUILT or unpinned inputs are errors; nothing is defaulted.
func ComputeMaterials(root string, lock []byte) (Materials, error) {
	out, _, err := computeMaterials(root, lock)
	return out, err
}

func computeMaterials(root string, lock []byte) (Materials, []byte, error) {
	var out Materials
	manifest, err := verifyInstallation(root)
	if err != nil {
		return out, nil, err
	}
	if out.ProfileDigest, err = Digest(json.RawMessage(manifest)); err != nil {
		return out, nil, err
	}
	if out.ToolchainDigest, err = toolchainBinding(lock, manifest); err != nil {
		return out, nil, err
	}
	if out.CallersDigest, out.TypesDigest, out.TestsDigest, err = contractDigests(manifest); err != nil {
		return out, nil, err
	}
	if out.UISHA, err = pinnedUI(root); err != nil {
		return out, nil, err
	}
	return out, manifest, nil
}
