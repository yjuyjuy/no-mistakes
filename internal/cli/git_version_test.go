package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/git"
)

// skipIfMergeTreeUnsupported skips a test whose assertions depend on the
// branch-sync equivalent-divergence proof, which calls `git merge-tree
// --write-tree --merge-base`. That option only exists since git 2.40, so on
// older git the proof errors out and the run classifies as plain divergence.
// The minimum is declared in internal/git alongside the doctor warning.
func skipIfMergeTreeUnsupported(t *testing.T) {
	t.Helper()
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		t.Fatalf("git --version failed: %v", err)
	}
	raw := strings.TrimSpace(string(out))
	major, minor, ok := git.ParseVersion(raw)
	if !ok {
		t.Fatalf("cannot parse `git --version` output %q", raw)
	}
	if major > git.MinVersionMajor || (major == git.MinVersionMajor && minor >= git.MinVersionMinor) {
		return
	}
	t.Skipf("git >= %d.%d required, found %s: branch-sync equivalent-divergence uses `git merge-tree --write-tree --merge-base`, which only exists since git %d.%d",
		git.MinVersionMajor, git.MinVersionMinor, raw, git.MinVersionMajor, git.MinVersionMinor)
}
