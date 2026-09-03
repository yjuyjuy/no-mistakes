package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/telemetry"
)

// TestDoctorWarnsOnGitOlderThanMinimum proves `no-mistakes doctor` flags a git
// older than the branch-sync minimum (2.40) without failing the overall check:
// the merge-tree custody-recovery proof needs 2.40, so an operator on an older
// git should be told, but git is still functional for everything else.
func TestDoctorWarnsOnGitOlderThanMinimum(t *testing.T) {
	restore := telemetry.SetDefaultForTesting(&telemetryRecorder{})
	defer restore()

	t.Setenv("NM_HOME", t.TempDir())

	binDir := t.TempDir()
	writeFakeGitVersion(t, binDir, "git version 2.39.5")
	writeDoctorStubBinary(t, binDir, "acpx")
	t.Setenv("PATH", binDir)

	out, _ := executeCmd("doctor")
	if !strings.Contains(out, "git 2.39 is older than the required git >= 2.40") {
		t.Fatalf("doctor did not warn about the old git version:\n%s", out)
	}
}

// TestDoctorAcceptsGitAtOrAboveMinimum proves a git at the minimum draws no
// version warning.
func TestDoctorAcceptsGitAtOrAboveMinimum(t *testing.T) {
	restore := telemetry.SetDefaultForTesting(&telemetryRecorder{})
	defer restore()

	t.Setenv("NM_HOME", t.TempDir())

	binDir := t.TempDir()
	writeFakeGitVersion(t, binDir, "git version 2.40.0")
	writeDoctorStubBinary(t, binDir, "acpx")
	t.Setenv("PATH", binDir)

	out, _ := executeCmd("doctor")
	if strings.Contains(out, "older than the required git") {
		t.Fatalf("doctor warned about a new-enough git:\n%s", out)
	}
}

func writeFakeGitVersion(t *testing.T, dir, version string) {
	t.Helper()
	name := "git"
	contents := "#!/bin/sh\necho '" + version + "'\n"
	if runtime.GOOS == "windows" {
		name = "git.cmd"
		contents = "@echo off\r\necho " + version + "\r\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
}
