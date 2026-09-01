package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionInfoDefaults(t *testing.T) {
	// Save and restore the package-level build vars.
	origV, origC, origD := version, commit, date
	t.Cleanup(func() { version, commit, date = origV, origC, origD })

	version, commit, date = "", "", ""
	v, c, d := versionInfo()
	// With nothing injected, values must never be empty (fall back to
	// build info or the sentinel defaults).
	if v == "" || c == "" || d == "" {
		t.Fatalf("expected non-empty version fields, got v=%q c=%q d=%q", v, c, d)
	}
}

func TestVersionInfoLdflagsOverride(t *testing.T) {
	origV, origC, origD := version, commit, date
	t.Cleanup(func() { version, commit, date = origV, origC, origD })

	version, commit, date = "v9.9.9", "deadbeef", "2026-09-01"
	v, c, d := versionInfo()
	if v != "v9.9.9" || c != "deadbeef" || d != "2026-09-01" {
		t.Fatalf("expected injected values, got v=%q c=%q d=%q", v, c, d)
	}
}

func TestVersionCommandOutput(t *testing.T) {
	origV, origC, origD := version, commit, date
	t.Cleanup(func() { version, commit, date = origV, origC, origD })
	version, commit, date = "v1.2.3", "abc1234", "2026-09-01"

	cmd := newVersionCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	for _, want := range []string{"envd v1.2.3", "commit: abc1234", "built:  2026-09-01", "go:"} {
		if !strings.Contains(got, want) {
			t.Errorf("version output missing %q; got:\n%s", want, got)
		}
	}
}
