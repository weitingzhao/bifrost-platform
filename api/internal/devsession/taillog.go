package devsession

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

const tailReadChunk = 32 * 1024

// TailFileLines returns the last n lines of path without reading the whole file
// into memory when the file is large. n <= 0 defaults to 200.
func TailFileLines(path string, n int) ([]string, error) {
	if n <= 0 {
		n = 200
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	if size == 0 {
		return []string{}, nil
	}

	var (
		pos  = size
		buf  []byte
		found = 0
	)

	for pos > 0 && found <= n {
		readSize := int64(tailReadChunk)
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		chunk := make([]byte, readSize)
		if _, err := f.ReadAt(chunk, pos); err != nil && err != io.EOF {
			return nil, fmt.Errorf("readat: %w", err)
		}
		buf = append(chunk, buf...)
		found = bytes.Count(buf, []byte{'\n'})
		if found > n {
			break
		}
	}

	// Trim trailing newline so Split doesn't yield a final empty element.
	if len(buf) > 0 && buf[len(buf)-1] == '\n' {
		buf = buf[:len(buf)-1]
	}
	if len(buf) == 0 {
		return []string{}, nil
	}

	parts := bytes.Split(buf, []byte{'\n'})
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = string(p)
	}
	return out, nil
}
