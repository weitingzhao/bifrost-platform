package a

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceRule(t *testing.T) {
	files, err := filepath.Glob("../b/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "forbidden") {
			t.Fatal("forbidden word")
		}
	}
}
