package branchsync

import (
	"os/exec"
	"strings"
	"testing"

	gitpkg "github.com/kunchenguid/no-mistakes/internal/git"
)

// skipIfMergeTreeUnsupported skips a test whose assertions depend on the
// equivalent-divergence and custody-recovery proofs in sync.go, which call
// `git merge-tree --write-tree --merge-base` (equivalentDivergence /
// preservedContainsLocalWork). The option only exists since git 2.40; on older
// git every such expectation fails - or falsely passes - for the wrong reason,
// because the merge-tree call errors out instead of comparing trees. The
// minimum is declared in internal/git so the warning surface and the docs name
// the same version.
func skipIfMergeTreeUnsupported(t *testing.T) {
	t.Helper()
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		t.Fatalf("git --version failed: %v", err)
	}
	raw := strings.TrimSpace(string(out))
	major, minor, ok := gitpkg.ParseVersion(raw)
	if !ok {
		t.Fatalf("cannot parse `git --version` output %q", raw)
	}
	if major > gitpkg.MinVersionMajor || (major == gitpkg.MinVersionMajor && minor >= gitpkg.MinVersionMinor) {
		return
	}
	t.Skipf("git >= %d.%d required, found %s: the branchsync equivalent-divergence and custody-recovery proofs use `git merge-tree --write-tree --merge-base`, which only exists since git %d.%d",
		gitpkg.MinVersionMajor, gitpkg.MinVersionMinor, raw, gitpkg.MinVersionMajor, gitpkg.MinVersionMinor)
}
