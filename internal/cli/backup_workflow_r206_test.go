package cli

import (
	"os"
	"strings"
	"testing"
)

// F6830: the scheduled "Backup and Restore" workflow failed on every run
// because `go build` ran without the git-ignored web/*/dist directories that
// embed.go embeds (embed.go: pattern web/account/dist/*: no matching files).
func TestBackupWorkflowBuildsWebBeforeGoBuild(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/backup-restore.yml")
	if err != nil {
		t.Skipf("workflow not available: %v", err)
	}
	wf := string(raw)

	if got, want := strings.Count(wf, "go build "), strings.Count(wf, "- name: Build web UIs"); got != want || got == 0 {
		t.Fatalf("every job that runs go build needs a web UI build step: go build x%d, web builds x%d", got, want)
	}
	// Each web build step must precede the go build step that follows it.
	rest := wf
	for {
		g := strings.Index(rest, "go build ")
		if g < 0 {
			break
		}
		w := strings.LastIndex(rest[:g], "- name: Build web UIs")
		if w < 0 {
			t.Fatal("go build appears before any Build web UIs step")
		}
		rest = rest[g+len("go build "):]
	}
	for _, bad := range []string{"--compress", "--encrypt", "pull_request:", "\n  push:"} {
		if strings.Contains(wf, bad) {
			t.Errorf("workflow contains %q", bad)
		}
	}
}
