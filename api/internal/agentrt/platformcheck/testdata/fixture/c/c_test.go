package c

import (
	"os"
	"testing"
)

func TestDatabase(t *testing.T) {
	if os.Getenv("AGENTRT_TEST_PG") == "" {
		t.Skip("no database")
	}
	if Value != 1 {
		t.Fatal("bad database value")
	}
}
