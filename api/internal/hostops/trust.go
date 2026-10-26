package hostops

import (
	"bytes"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"path"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

const (
	NamespaceInventory = "bifrost-host-ops-inventory"
	NamespaceBundle    = "bifrost-host-ops-bundle"
	NamespacePolicy    = "bifrost-host-ops-policy"
	NamespaceApproval  = "bifrost-host-ops-approval"
	NamespaceUnfreeze  = "bifrost-host-ops-unfreeze"
)

// No production anchor or signing API is installed by S1. Anchors must come
// from the root-owned trust installation, never from a submitted artifact.
type Trust struct{ Anchor string }

type Signed struct {
	Text      string `json:"text"`
	Signature string `json:"signature"`
}

func (t Trust) verify(s Signed, namespace string) error {
	if len(s.Text) > MaxDocument || len(s.Signature) > 16384 {
		return errors.New("oversized signed artifact")
	}
	p, rest := pem.Decode([]byte(s.Signature))
	if p == nil || len(bytes.TrimSpace(rest)) != 0 || len(p.Headers) != 0 {
		return errors.New("one SSHSIG required")
	}
	return releasepolicy.VerifyAnchored([]byte(s.Signature), []byte(s.Text), namespace, t.Anchor)
}

type Metadata struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	Version       uint64 `json:"version"`
	Environment   string `json:"environment"`
	EffectiveAt   string `json:"effective_at"`
	ExpiresAt     string `json:"expires_at"`
}

func (m Metadata) validate() error {
	if m.SchemaVersion != 1 || m.ID == "" || m.Version == 0 || !contains([]string{"test", "dev", "stg", "prod"}, m.Environment) {
		return errors.New("invalid artifact identity")
	}
	_, _, err := times(m.EffectiveAt, m.ExpiresAt)
	return err
}

type Host struct {
	ID          string   `json:"id"`
	Environment string   `json:"environment"`
	OS          string   `json:"os"`
	HostKey     string   `json:"host_key"`
	MAC         string   `json:"mac"`
	IP          string   `json:"ip"`
	Lifecycle   string   `json:"lifecycle"`
	Generation  uint64   `json:"generation"`
	ExecutorIDs []string `json:"executor_ids"`
}

func hostFingerprint(s string) bool {
	if !strings.HasPrefix(s, "SHA256:") {
		return false
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, "SHA256:"))
	return err == nil && len(b) == 32
}

// Objects cover service/project, root/install, endpoint/repo_set and protected
// registry entries. Attributes are explicit, typed and part of the signature.
type Object struct {
	ID                      string            `json:"id"`
	Host                    string            `json:"host"`
	Kind                    string            `json:"kind"`
	Canonical               string            `json:"canonical"`
	Aliases                 []string          `json:"aliases"`
	ConfigDigest            string            `json:"config_digest"`
	Stateful                bool              `json:"stateful"`
	Dependencies            []string          `json:"dependencies"`
	Actions                 []string          `json:"actions"`
	MaintenanceScope        []string          `json:"maintenance_scope"`
	Path                    string            `json:"path"`
	Owner                   string            `json:"owner"`
	Purpose                 string            `json:"purpose"`
	RebuildProof            string            `json:"rebuild_proof"`
	Files                   []string          `json:"files"`
	InstallationExecutionID string            `json:"installation_execution_id"`
	Address                 string            `json:"address"`
	Ports                   []int             `json:"ports"`
	Methods                 []string          `json:"methods"`
	Repositories            map[string]string `json:"repositories"`
}

type Inventory struct {
	Meta         Metadata `json:"meta"`
	Hosts        []Host   `json:"hosts"`
	Objects      []Object `json:"objects"`
	ProtectedIDs []string `json:"protected_ids"`
}

func (i Inventory) validate() error {
	if err := i.Meta.validate(); err != nil {
		return err
	}
	hosts, ids, aliases := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, h := range i.Hosts {
		mac, macErr := net.ParseMAC(h.MAC)
		if h.ID == "" || hosts[h.ID] || h.Environment != i.Meta.Environment || !contains([]string{"linux", "macos", "ugos"}, h.OS) || !hostFingerprint(h.HostKey) || net.ParseIP(h.IP) == nil || macErr != nil || len(mac) != 6 || h.Generation == 0 || !setOK(h.ExecutorIDs, false) || !contains([]string{"new", "out-of-cluster", "joining", "in-cluster", "unknown", "retired"}, h.Lifecycle) {
			return errors.New("invalid host")
		}
		hosts[h.ID] = true
	}
	if len(hosts) == 0 || !setOK(i.ProtectedIDs, true) {
		return errors.New("invalid inventory")
	}
	for _, o := range i.Objects {
		if o.ID == "" || ids[o.ID] || !hosts[o.Host] || !contains([]string{"service", "project", "root", "install", "endpoint", "repo_set", "protected"}, o.Kind) || o.Canonical == "" || !validDigest(o.ConfigDigest) || !setOK(o.Aliases, true) || !setOK(o.Actions, false) || !setOK(o.Dependencies, true) || !setOK(o.MaintenanceScope, true) {
			return errors.New("invalid registered object")
		}
		ids[o.ID] = true
		for _, name := range append([]string{o.Canonical}, o.Aliases...) {
			k := o.Host + "/" + name
			if aliases[k] {
				return errors.New("ambiguous object alias")
			}
			aliases[k] = true
		}
		for _, port := range o.Ports {
			if port < 1 || port > 65535 {
				return errors.New("invalid endpoint port")
			}
		}
		if (o.Canonical == "minio" || o.Canonical == "gitea" || o.Canonical == "registry") && !o.Stateful {
			return errors.New("known stateful project must be stateful")
		}
	}
	for _, o := range i.Objects {
		for _, dep := range o.Dependencies {
			if !ids[dep] {
				return errors.New("unknown dependency")
			}
		}
	}
	for _, id := range i.ProtectedIDs {
		if !ids[id] {
			return errors.New("unknown protected object")
		}
	}
	return nil
}

func (t Trust) Inventory(s Signed) (Inventory, error) {
	var i Inventory
	if err := t.verify(s, NamespaceInventory); err != nil {
		return i, err
	}
	if err := Decode([]byte(s.Text), &i); err != nil {
		return i, err
	}
	return i, i.validate()
}

type BundleEntry struct {
	Path      string `json:"path"`
	Size      uint64 `json:"size"`
	SHA256    string `json:"sha256"`
	Owner     string `json:"owner"`
	Mode      int    `json:"mode"`
	Purpose   string `json:"purpose"`
	OCIDigest string `json:"oci_digest"`
}
type Bundle struct {
	Meta           Metadata      `json:"meta"`
	Actions        []string      `json:"actions"`
	Hosts          []string      `json:"hosts"`
	HandlerDigests []string      `json:"handler_digests"`
	Entries        []BundleEntry `json:"entries"`
}

func safeRelative(p string) bool {
	return p != "" && p != "." && !strings.ContainsAny(p, "\\\x00") && !path.IsAbs(p) && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}
func (b Bundle) validate() error {
	if err := b.Meta.validate(); err != nil {
		return err
	}
	if !setOK(b.Actions, false) || !setOK(b.Hosts, false) || !setOK(b.HandlerDigests, false) || len(b.Entries) == 0 {
		return errors.New("empty bundle scope")
	}
	for _, d := range b.HandlerDigests {
		if !validDigest(d) {
			return errors.New("invalid handler digest")
		}
	}
	seen := map[string]bool{}
	for _, e := range b.Entries {
		if !safeRelative(e.Path) || seen[e.Path] || !validDigest(e.SHA256) || e.Owner != "root" || e.Mode < 0 || e.Mode > 0777 || e.Mode&0022 != 0 || !contains([]string{"executable", "config", "unit", "compose", "image", "rollback"}, e.Purpose) {
			return errors.New("invalid bundle entry")
		}
		if e.Purpose == "image" && !validDigest(e.OCIDigest) || e.Purpose != "image" && e.OCIDigest != "" {
			return errors.New("image requires immutable OCI digest")
		}
		seen[e.Path] = true
	}
	return nil
}
func (t Trust) Bundle(s Signed) (Bundle, error) {
	var b Bundle
	if err := t.verify(s, NamespaceBundle); err != nil {
		return b, err
	}
	if err := Decode([]byte(s.Text), &b); err != nil {
		return b, err
	}
	return b, b.validate()
}

// ReceivedFile is a receiver's observation, not request metadata. S3 must
// obtain it using protected file descriptors, and refuse links at extraction.
type ReceivedFile struct {
	Path    string
	Bytes   []byte
	Owner   string
	Mode    int
	Regular bool
	Links   uint64
}

func (b Bundle) VerifyFiles(files []ReceivedFile, environment, host, action, handler string, now time.Time) error {
	if err := b.validate(); err != nil {
		return err
	}
	if b.Meta.Environment != environment || !current(b.Meta.EffectiveAt, b.Meta.ExpiresAt, now) || !contains(b.Hosts, host) || !contains(b.Actions, action) || !contains(b.HandlerDigests, handler) {
		return errors.New("bundle outside signed scope")
	}
	if len(files) != len(b.Entries) {
		return errors.New("bundle file set differs")
	}
	seen := map[string]bool{}
	for _, f := range files {
		if !f.Regular || f.Links != 1 || seen[f.Path] {
			return errors.New("bundle link or duplicate")
		}
		seen[f.Path] = true
		found := false
		for _, e := range b.Entries {
			if e.Path == f.Path {
				found = true
				if uint64(len(f.Bytes)) != e.Size || Digest(f.Bytes) != e.SHA256 || f.Owner != e.Owner || f.Mode != e.Mode {
					return fmt.Errorf("bundle bytes/metadata differ: %s", f.Path)
				}
			}
		}
		if !found {
			return errors.New("unlisted bundle file")
		}
	}
	return nil
}

// ArtifactMark is persisted with the raw signed bytes in one transaction.
// Version equality is only an idempotent reload of exactly the installed bytes.
type ArtifactMark struct {
	Version uint64 `json:"version"`
	Digest  string `json:"digest"`
}

func CheckArtifact(meta Metadata, raw []byte, mark ArtifactMark, environment string, now time.Time) (ArtifactMark, error) {
	if err := meta.validate(); err != nil {
		return mark, err
	}
	d := Digest(raw)
	if meta.Environment != environment || !current(meta.EffectiveAt, meta.ExpiresAt, now) || meta.Version < mark.Version || meta.Version == mark.Version && d != mark.Digest {
		return mark, errors.New("artifact expired, wrong environment or rollback")
	}
	return ArtifactMark{meta.Version, d}, nil
}
