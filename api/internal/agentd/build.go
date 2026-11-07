package agentd

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
)

var commitHex = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ReadBuild is the build of the running agentd, whose binary is self: the
// commit Go stamped in it (vcs.revision, with vcs.modified) and the sha256 of
// the binary. A binary built without its commit (outside a git checkout, or
// with -buildvcs=false) has none to report: the platform takes a build only
// whole, so it is an error.
func ReadBuild(self string) (*Build, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, errors.New("agentd: no build information in this binary")
	}
	return buildOf(info, self)
}

func buildOf(info *debug.BuildInfo, self string) (*Build, error) {
	var b Build
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Commit = s.Value
		case "vcs.modified":
			b.Modified = s.Value == "true"
		}
	}
	if !commitHex.MatchString(b.Commit) {
		return nil, fmt.Errorf("agentd: this binary names no git commit it was built from (vcs.revision %q)", b.Commit)
	}
	f, err := os.Open(self)
	if err != nil {
		return nil, fmt.Errorf("agentd: the sha256 of the binary: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("agentd: the sha256 of the binary: %w", err)
	}
	b.SHA256 = hex.EncodeToString(h.Sum(nil))
	return &b, nil
}
