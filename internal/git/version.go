package git

import (
	"strconv"
	"strings"
)

// Minimum Git version required by the branchsync equivalent-divergence and
// custody-recovery proofs, which call `git merge-tree --write-tree
// --merge-base` (added in git 2.40). Kept here so the runtime warning surface,
// the test skip guard, and the documentation all name the same minimum.
const (
	MinVersionMajor = 2
	MinVersionMinor = 40
)

// ParseVersion extracts the major and minor from `git --version` output such
// as "git version 2.40.0" or "git version 2.39.5 (Apple Git-140)". It returns
// ok=false for any unrecognized shape, so callers can keep treating an
// unparseable report as "version unknown" instead of guessing.
func ParseVersion(line string) (major, minor int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return 0, 0, false
	}
	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return 0, 0, false
	}
	return major, minor, true
}
