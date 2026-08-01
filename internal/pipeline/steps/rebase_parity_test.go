package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/agent"
	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// rebaseDropScenario builds the incident's exact shape in a temp repo:
//   - base: app.js calls doThing(); mapper.js exports the helper.
//   - origin/main advances with a conflicting edit to app.js's same region.
//   - feature (off base) wires the helper call into app.js AND adds an
//     unconflicted guard test that references the helper.
//
// The returned repo is checked out on feature with origin/main advanced, so a
// RebaseStep in fixing mode will hit a conflict in app.js only. The caller's
// mock agent supplies the resolution.
func rebaseDropScenario(t *testing.T) (dir, upstream, baseSHA, headSHA string) {
	t.Helper()
	upstream = t.TempDir()
	gitCmd(t, upstream, "init", "--bare")

	dir = t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "checkout", "-b", "main")
	gitCmd(t, dir, "remote", "add", "origin", upstream)

	writeFile(t, dir, "app.js", "import { buildRecordPriceLayer } from \"./mapper.js\";\nfunction main() {\n  doThing();\n}\n")
	writeFile(t, dir, "mapper.js", "export function buildRecordPriceLayer(){ return 1 }\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base commit")
	baseSHA = gitCmd(t, dir, "rev-parse", "HEAD")
	gitCmd(t, dir, "push", "origin", "main")

	// feature off base: wire the call site + add an unconflicted guard test.
	gitCmd(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "app.js", "import { buildRecordPriceLayer } from \"./mapper.js\";\nfunction main() {\n  doThing();\n  buildRecordPriceLayer();\n}\n")
	writeFile(t, dir, "guard.test.js", "test(\"wires buildRecordPriceLayer into app\", () => {});\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "wire call site + guard test")
	headSHA = gitCmd(t, dir, "rev-parse", "HEAD")

	// origin/main advances with a conflicting edit to app.js's same region.
	gitCmd(t, dir, "checkout", "main")
	writeFile(t, dir, "app.js", "import { buildRecordPriceLayer } from \"./mapper.js\";\nfunction main() {\n  doThing();\n  logConflicting();\n}\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "upstream logConflicting")
	gitCmd(t, dir, "push", "origin", "main")
	gitCmd(t, dir, "checkout", "feature")

	return dir, upstream, baseSHA, headSHA
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// resolveConflictAgent returns a mock agent that resolves the app.js rebase
// conflict by writing resolved, staging app.js, and running rebase --continue.
func resolveConflictAgent(t *testing.T, dir, resolved string) *mockAgent {
	t.Helper()
	return &mockAgent{
		name: "test",
		runFn: func(ctx context.Context, opts agent.RunOpts) (*agent.Result, error) {
			writeFile(t, dir, "app.js", resolved)
			env := append(os.Environ(),
				"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
				"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
				"GIT_EDITOR=true",
			)
			add := exec.Command("git", "add", "app.js")
			add.Dir = dir
			add.Env = env
			if out, err := add.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("git add: %s: %w", out, err)
			}
			cont := exec.Command("git", "rebase", "--continue")
			cont.Dir = dir
			cont.Env = env
			if out, err := cont.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("git rebase --continue: %s: %w", out, err)
			}
			return &agent.Result{Output: json.RawMessage(`{"summary":"resolve conflict"}`)}, nil
		},
	}
}

// TestRebaseStep_DroppedFeatureHunkParksInsteadOfPassing is the P0 regression:
// a conflict resolution that keeps only the upstream side (dropping the
// feature's wired call site) while leaving the unconflicted guard test intact
// must PARK the run for a human, not report success. Without the parity check
// the rebase completes cleanly and the step returns a non-approval outcome,
// which is exactly the green-pass-on-reverted-work incident.
func TestRebaseStep_DroppedFeatureHunkParksInsteadOfPassing(t *testing.T) {
	t.Parallel()
	dir, upstream, baseSHA, headSHA := rebaseDropScenario(t)

	// BAD resolution: keep only the upstream side, dropping buildRecordPriceLayer().
	badResolution := "import { buildRecordPriceLayer } from \"./mapper.js\";\nfunction main() {\n  doThing();\n  logConflicting();\n}\n"
	ag := resolveConflictAgent(t, dir, badResolution)

	sctx := newTestContextWithDBRecords(t, ag, dir, baseSHA, headSHA, config.Commands{})
	sctx.Run.Branch = "refs/heads/feature"
	sctx.Repo.UpstreamURL = upstream
	sctx.Fixing = true
	sctx.PreviousFindings = `{"findings":[{"severity":"warning","file":"app.js","description":"merge conflict rebasing onto origin/main"}]}`

	outcome, err := (&RebaseStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome == nil || !outcome.NeedsApproval {
		t.Fatalf("expected NeedsApproval after a dropped feature hunk, got %#v", outcome)
	}
	if outcome.AutoFixable {
		t.Fatal("a dropped hunk must not be auto-fixable; it must park for a human")
	}
	if !strings.Contains(outcome.Findings, "buildRecordPriceLayer()") {
		t.Fatalf("expected findings to name the dropped line, got: %s", outcome.Findings)
	}
	findings, perr := types.ParseFindingsJSON(outcome.Findings)
	if perr != nil {
		t.Fatalf("parse findings: %v", perr)
	}
	if len(findings.Items) == 0 || findings.Items[0].Action != types.ActionAskUser {
		t.Fatalf("expected an ask-user finding, got: %#v", findings.Items)
	}
	// Confirm the tree really lost the call site (the incident precondition).
	app, _ := os.ReadFile(filepath.Join(dir, "app.js"))
	if strings.Contains(string(app), "buildRecordPriceLayer();") {
		t.Fatal("test setup invalid: call site was not actually dropped")
	}
	if _, err := os.Stat(filepath.Join(dir, "guard.test.js")); err != nil {
		t.Fatal("test setup invalid: unconflicted guard test should survive")
	}
}

// TestRebaseStep_CorrectResolutionDoesNotFalsePositive proves the parity check
// does not block a legitimate conflict resolution that preserves both sides.
// A correct resolution keeps logConflicting() AND buildRecordPriceLayer(), so
// no author line vanishes and the run proceeds.
func TestRebaseStep_CorrectResolutionDoesNotFalsePositive(t *testing.T) {
	t.Parallel()
	dir, upstream, baseSHA, headSHA := rebaseDropScenario(t)

	goodResolution := "import { buildRecordPriceLayer } from \"./mapper.js\";\nfunction main() {\n  doThing();\n  logConflicting();\n  buildRecordPriceLayer();\n}\n"
	ag := resolveConflictAgent(t, dir, goodResolution)

	sctx := newTestContextWithDBRecords(t, ag, dir, baseSHA, headSHA, config.Commands{})
	sctx.Run.Branch = "refs/heads/feature"
	sctx.Repo.UpstreamURL = upstream
	sctx.Fixing = true
	sctx.PreviousFindings = `{"findings":[{"severity":"warning","file":"app.js","description":"merge conflict rebasing onto origin/main"}]}`

	outcome, err := (&RebaseStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != nil && outcome.NeedsApproval {
		t.Fatalf("a correct resolution must not park; got findings: %s", outcome.Findings)
	}
	app, _ := os.ReadFile(filepath.Join(dir, "app.js"))
	if !strings.Contains(string(app), "buildRecordPriceLayer();") {
		t.Fatal("correct resolution should keep the feature call site")
	}
	if !strings.Contains(string(app), "logConflicting();") {
		t.Fatal("correct resolution should keep the upstream edit")
	}
}

// TestRebaseStep_CleanRebaseDoesNotFalsePositive proves the fast path: a rebase
// with no conflict (upstream touched a different file) preserves every hunk by
// patch-id, so Signal A short-circuits and the parity check never fires.
func TestRebaseStep_CleanRebaseDoesNotFalsePositive(t *testing.T) {
	t.Parallel()
	upstream := t.TempDir()
	gitCmd(t, upstream, "init", "--bare")

	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "checkout", "-b", "main")
	gitCmd(t, dir, "remote", "add", "origin", upstream)
	writeFile(t, dir, "app.js", "function main() {\n  doThing();\n}\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	baseSHA := gitCmd(t, dir, "rev-parse", "HEAD")
	gitCmd(t, dir, "push", "origin", "main")

	gitCmd(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "app.js", "function main() {\n  doThing();\n  buildRecordPriceLayer();\n}\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "wire call site")
	headSHA := gitCmd(t, dir, "rev-parse", "HEAD")

	// upstream advances a DIFFERENT file (no conflict).
	gitCmd(t, dir, "checkout", "main")
	writeFile(t, dir, "README.md", "docs\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "upstream unrelated")
	gitCmd(t, dir, "push", "origin", "main")
	gitCmd(t, dir, "checkout", "feature")

	ag := &mockAgent{name: "test"}
	sctx := newTestContextWithDBRecords(t, ag, dir, baseSHA, headSHA, config.Commands{})
	sctx.Run.Branch = "refs/heads/feature"
	sctx.Repo.UpstreamURL = upstream

	outcome, err := (&RebaseStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != nil && outcome.NeedsApproval {
		t.Fatalf("a clean rebase must not park; got findings: %s", outcome.Findings)
	}
	if len(ag.calls) != 0 {
		t.Fatalf("clean rebase should not call the agent, got %d calls", len(ag.calls))
	}
	app, _ := os.ReadFile(filepath.Join(dir, "app.js"))
	if !strings.Contains(string(app), "buildRecordPriceLayer();") {
		t.Fatal("clean rebase should keep the feature call site")
	}
}
