package delivery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The platform reads Gitea (branches, tags, commits, compare, raw files, the
// org's repositories, upload-pack advertisements) with a Gitea account of
// its own that can read the organization's repositories and do nothing else
// (TD-432). Its basic-auth Secret (kubernetes.io/basic-auth, keys username
// and password) lives in the platform's own namespace and is mounted into
// platform-api and platform-workers; GITEA_READER_DIR names the mount. The
// platform holds no Kubernetes permission on any Secret for this, and it
// never reads the Gitea administrator's token: without the reader every
// Gitea read reports that it has no credential instead of trying another
// one.
const giteaReaderDirEnv = "GITEA_READER_DIR"

// maxReaderFile bounds one credential file; a Gitea token is 40 characters.
const maxReaderFile = 4096

// errNoGiteaReader is the answer when GITEA_READER_DIR is not set.
var errNoGiteaReader = errors.New(giteaReaderDirEnv + " is not set: the platform has no Gitea reader credential (TD-432)")

// giteaReaderFromEnv reads the reader's username and password from the files
// of GITEA_READER_DIR, anew on every call, so a rotated Secret is used as soon
// as the kubelet has updated the mount. Errors name the file, never its text.
func giteaReaderFromEnv(context.Context) (string, string, error) {
	dir := strings.TrimSpace(os.Getenv(giteaReaderDirEnv))
	if dir == "" {
		return "", "", errNoGiteaReader
	}
	user, err := readReaderFile(dir, "username")
	if err != nil {
		return "", "", err
	}
	if strings.Contains(user, ":") {
		return "", "", fmt.Errorf("gitea reader: %s/username holds a ':', which basic auth cannot carry", dir)
	}
	pass, err := readReaderFile(dir, "password")
	if err != nil {
		return "", "", err
	}
	return user, pass, nil
}

// readReaderFile is one credential file's single line, trimmed.
func readReaderFile(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("gitea reader: %s is missing (is the Secret there and mounted?)", path)
		}
		return "", fmt.Errorf("gitea reader: cannot open %s", path)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxReaderFile+1))
	if err != nil {
		return "", fmt.Errorf("gitea reader: cannot read %s", path)
	}
	if len(raw) > maxReaderFile {
		return "", fmt.Errorf("gitea reader: %s is longer than %d bytes", path, maxReaderFile)
	}
	v := strings.TrimSpace(string(raw))
	if v == "" {
		return "", fmt.Errorf("gitea reader: %s is empty", path)
	}
	if strings.ContainsAny(v, "\r\n\x00") {
		return "", fmt.Errorf("gitea reader: %s holds more than one line", path)
	}
	return v, nil
}

// giteaCredentials is the basic-auth pair every Gitea read of this service
// sends. A Service built by NewService has no reader and reads anonymously
// (tests, and services that never read a private repository); the handler
// the server runs (NewHandler) reads GITEA_READER_DIR. An error means Gitea
// cannot be asked: callers report it as unknown, never as "absent".
func (s *Service) giteaCredentials(ctx context.Context) (string, string, error) {
	if s.readerCreds == nil {
		return "", "", nil
	}
	return s.readerCreds(ctx)
}

// giteaReaderConfigured says whether this service has a reader credential it
// can read now (the supply chain view's mirror_credentials_configured).
func (s *Service) giteaReaderConfigured(ctx context.Context) bool {
	if s.readerCreds == nil {
		return false
	}
	user, pass, err := s.readerCreds(ctx)
	return err == nil && user != "" && pass != ""
}
