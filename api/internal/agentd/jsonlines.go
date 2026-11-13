package agentd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// readJSONLines reads a file of one JSON value a line; none is no file.
func readJSONLines[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var v T
		if err = json.Unmarshal(sc.Bytes(), &v); err != nil {
			return nil, fmt.Errorf("agentd: %s: %w", path, err)
		}
		out = append(out, v)
	}
	return out, sc.Err()
}

// appendJSONLine adds v to a file of one JSON value a line.
func appendJSONLine(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
