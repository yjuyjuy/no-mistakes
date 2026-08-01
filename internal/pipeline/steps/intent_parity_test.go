package steps

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func initParityRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "checkout", "-b", "main")
	return dir
}

func TestIsSignificantSourceLine(t *testing.T) {
	t.Parallel()
	significant := []string{
		"buildRecordPriceLayer();",
		"import { x } from \"./y.js\";",
		"const value = compute(input)",
	}
	for _, line := range significant {
		if !isSignificantSourceLine(strings.TrimSpace(line)) {
			t.Errorf("expected %q to be significant", line)
		}
	}
	trivial := []string{"", "}", "{", "};", ")", "]", "else {", "return;", "ab", "//"}
	for _, line := range trivial {
		if isSignificantSourceLine(strings.TrimSpace(line)) {
			t.Errorf("expected %q to be trivial", line)
		}
	}
}

func TestIdentifiers(t *testing.T) {
	t.Parallel()
	got := identifiers("  buildRecordPriceLayer(config);")
	want := map[string]bool{"buildrecordpricelayer": true, "config": true}
	for _, id := range got {
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing identifiers %v in %v", want, got)
	}
	// Short tokens (<3 chars) are excluded.
	for _, id := range identifiers("a = b + c") {
		if len(id) < 3 {
			t.Fatalf("unexpected short identifier %q", id)
		}
	}
}

func TestDetectDroppedAuthorLines(t *testing.T) {
	t.Parallel()
	dir := initParityRepo(t)
	writeFile(t, dir, "app.js", "function main() {\n  doThing();\n}\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	base := gitCmd(t, dir, "rev-parse", "HEAD")

	// preHead adds a significant line.
	writeFile(t, dir, "app.js", "function main() {\n  doThing();\n  buildRecordPriceLayer();\n}\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "add call")
	preHead := gitCmd(t, dir, "rev-parse", "HEAD")

	// postHead reverts that line.
	writeFile(t, dir, "app.js", "function main() {\n  doThing();\n}\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "revert call")
	postHead := gitCmd(t, dir, "rev-parse", "HEAD")

	lines, files, err := detectDroppedAuthorLines(context.Background(), dir, base, preHead, postHead)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "buildRecordPriceLayer()") {
		t.Fatalf("expected the dropped call line, got %v", lines)
	}
	if len(files) != 1 || files[0] != "app.js" {
		t.Fatalf("expected app.js as the affected file, got %v", files)
	}

	// When postHead keeps the line, nothing is dropped.
	keepLines, _, err := detectDroppedAuthorLines(context.Background(), dir, base, preHead, preHead)
	if err != nil {
		t.Fatal(err)
	}
	if len(keepLines) != 0 {
		t.Fatalf("expected no dropped lines when head unchanged, got %v", keepLines)
	}
}

func TestDetectRebaseHunkDrop_CleanReplayReturnsNil(t *testing.T) {
	t.Parallel()
	// A clean cherry-pick style replay preserves patch-ids, so Signal A
	// short-circuits and no drop is reported even though SHAs differ.
	dir := initParityRepo(t)
	writeFile(t, dir, "a.txt", "base\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	base := gitCmd(t, dir, "rev-parse", "HEAD")

	gitCmd(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "a.txt", "base\nfeatureContent = compute()\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "feature")
	preHead := gitCmd(t, dir, "rev-parse", "HEAD")

	// Rebuild the same change on a new base (different SHA, same patch-id).
	gitCmd(t, dir, "checkout", "main")
	writeFile(t, dir, "b.txt", "unrelated\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "unrelated")
	gitCmd(t, dir, "checkout", "feature")
	runGit(t, dir, "rebase", "main")
	postHead := gitCmd(t, dir, "rev-parse", "HEAD")

	if drop := detectRebaseHunkDrop(context.Background(), dir, base, preHead, postHead); drop != nil {
		t.Fatalf("expected nil for a clean replay, got %#v", drop)
	}
}

func TestDetectRebaseHunkDrop_FailsOpenOnMissingRevs(t *testing.T) {
	t.Parallel()
	dir := initParityRepo(t)
	writeFile(t, dir, "a.txt", "base\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	head := gitCmd(t, dir, "rev-parse", "HEAD")
	if drop := detectRebaseHunkDrop(context.Background(), dir, head, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", head); drop != nil {
		t.Fatalf("expected nil (fail-open) for a missing pre-head, got %#v", drop)
	}
}

// runGit runs git and fails the test on error; a thin wrapper used where the
// output is not needed.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		"GIT_EDITOR=true",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
