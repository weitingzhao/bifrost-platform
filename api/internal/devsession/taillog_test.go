package devsession

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTailFileLines_EmptyAndMissing(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := TailFileLines(empty, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("empty: got %v", lines)
	}

	_, err = TailFileLines(filepath.Join(dir, "missing.log"), 10)
	if !os.IsNotExist(err) {
		t.Fatalf("missing: want NotExist, got %v", err)
	}
}

func TestTailFileLines_LastN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	var body []byte
	for i := 1; i <= 1000; i++ {
		body = append(body, []byte("line-"+itoa(i)+"\n")...)
	}
	// Make file larger than one chunk
	pad := make([]byte, 40*1024)
	for i := range pad {
		pad[i] = 'x'
	}
	pad[len(pad)-1] = '\n'
	body = append(pad, body...)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := TailFileLines(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Fatalf("len=%d want 5: %v", len(lines), lines)
	}
	want := []string{"line-996", "line-997", "line-998", "line-999", "line-1000"}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines[%d]=%q want %q", i, lines[i], want[i])
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
