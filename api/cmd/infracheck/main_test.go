package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/infracheck"
)

func TestPrintCheckGolden(t *testing.T) {
	want, err := os.ReadFile("../../internal/agentrt/infracheck/testdata/check.golden")
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"--print-check"}, &out, &stderr); code != 0 || !bytes.Equal(out.Bytes(), want) || stderr.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), stderr.String())
	}
}

func TestPrintSetup(t *testing.T) {
	for _, wheelhouse := range []string{"", "/opt/wheels", "/tmp/it's a wheelhouse; $(false)"} {
		var out, stderr bytes.Buffer
		args := []string{"--print-setup"}
		if wheelhouse != "" {
			args = append(args, "--wheelhouse", wheelhouse)
		}
		if code := run(args, &out, &stderr); code != 0 || out.String() != infracheck.SetupScript(wheelhouse) {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), stderr.String())
		}
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"--print-check", "--print-setup"}, {"--print-check", "extra"}, {"--print-check", "--wheelhouse", "/opt/wheels"}, {"--unknown"}, {"--print-setup", "--wheelhouse"}} {
		var out, stderr bytes.Buffer
		if code := run(args, &out, &stderr); code != 2 || out.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), stderr.String())
		}
	}
}
