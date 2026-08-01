package steps

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/agent"
	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// reviewRevertScenario sets up a feature branch that contributed two behaviors
// in app.js (a validation guard and a wired call), checked out detached at the
// feature head so the review step runs in fix mode against it.
func reviewRevertScenario(t *testing.T) (dir, baseSHA, headSHA string) {
	t.Helper()
	dir = t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "checkout", "-b", "main")
	writeFile(t, dir, "app.js", "function main() {\n  doThing();\n}\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	baseSHA = gitCmd(t, dir, "rev-parse", "HEAD")

	gitCmd(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "app.js", "function main() {\n  validateRecordPriceLayer(input);\n  doThing();\n  buildRecordPriceLayer();\n}\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "wire validation + call")
	headSHA = gitCmd(t, dir, "rev-parse", "HEAD")
	gitCmd(t, dir, "checkout", "--detach", headSHA)
	return dir, baseSHA, headSHA
}

// TestReviewStep_FixRoundRevertingAuthorWorkParks is the review-loop analogue of
// the rebase parity regression: a fixer round that silently REVERTS the author's
// contributed lines (removes them without rewriting them in place) must park for
// a human rather than proceed to a clean rereview.
func TestReviewStep_FixRoundRevertingAuthorWorkParks(t *testing.T) {
	t.Parallel()
	dir, baseSHA, headSHA := reviewRevertScenario(t)

	callCount := 0
	ag := &mockAgent{
		name: "test",
		runFn: func(ctx context.Context, opts agent.RunOpts) (*agent.Result, error) {
			callCount++
			if callCount == 1 {
				// Fixer reverts both contributed behaviors back to base content.
				writeFile(t, dir, "app.js", "function main() {\n  doThing();\n}\n")
				return &agent.Result{Output: json.RawMessage(`{"summary":"remove flagged code"}`)}, nil
			}
			// A rereview, if reached, would report clean - which is the bug.
			j, _ := json.Marshal(Findings{Summary: "all clear"})
			return &agent.Result{Output: j}, nil
		},
	}

	sctx := newTestContextWithDBRecords(t, ag, dir, baseSHA, headSHA, config.Commands{})
	sctx.Run.Branch = "refs/heads/feature"
	sctx.Fixing = true
	sctx.PreviousFindings = `{"findings":[{"severity":"warning","file":"app.js","description":"buildRecordPriceLayer may be undefined"}]}`

	outcome, err := (&ReviewStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome == nil || !outcome.NeedsApproval {
		t.Fatalf("expected NeedsApproval after a reverting fix round, got %#v", outcome)
	}
	if outcome.AutoFixable {
		t.Fatal("a reverting fix round must not be auto-fixable; it must park")
	}
	if callCount != 1 {
		t.Fatalf("expected the backstop to park before rereview (1 agent call), got %d", callCount)
	}
	findings, perr := types.ParseFindingsJSON(outcome.Findings)
	if perr != nil {
		t.Fatalf("parse findings: %v", perr)
	}
	if len(findings.Items) == 0 || findings.Items[0].Action != types.ActionAskUser {
		t.Fatalf("expected an ask-user finding, got: %#v", findings.Items)
	}
	if !strings.Contains(outcome.Findings, "buildRecordPriceLayer()") {
		t.Fatalf("expected findings to name a reverted line, got: %s", outcome.Findings)
	}
}

// TestReviewStep_FixRoundModifyingAuthorLineDoesNotFalsePositive proves the
// backstop does not block a legitimate fix that rewrites an author line in
// place. The fixer changes the call's arguments (same identifiers reappear), so
// no author work is considered reverted and the run proceeds to rereview.
func TestReviewStep_FixRoundModifyingAuthorLineDoesNotFalsePositive(t *testing.T) {
	t.Parallel()
	dir, baseSHA, headSHA := reviewRevertScenario(t)

	callCount := 0
	ag := &mockAgent{
		name: "test",
		runFn: func(ctx context.Context, opts agent.RunOpts) (*agent.Result, error) {
			callCount++
			if callCount == 1 {
				// Legitimate forward fix: harden the same behaviors in place.
				writeFile(t, dir, "app.js", "function main() {\n  validateRecordPriceLayer(input, options);\n  doThing();\n  buildRecordPriceLayer(config);\n}\n")
				return &agent.Result{Output: json.RawMessage(`{"summary":"harden call arguments"}`)}, nil
			}
			j, _ := json.Marshal(Findings{Summary: "all clear"})
			return &agent.Result{Output: j}, nil
		},
	}

	sctx := newTestContextWithDBRecords(t, ag, dir, baseSHA, headSHA, config.Commands{})
	sctx.Run.Branch = "refs/heads/feature"
	sctx.Fixing = true
	sctx.PreviousFindings = `{"findings":[{"severity":"warning","file":"app.js","description":"pass options to the calls"}]}`

	outcome, err := (&ReviewStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != nil && outcome.NeedsApproval {
		t.Fatalf("a forward fix must not park; got findings: %s", outcome.Findings)
	}
	if callCount != 2 {
		t.Fatalf("expected fix + rereview (2 agent calls), got %d", callCount)
	}
	app, _ := os.ReadFile(filepath.Join(dir, "app.js"))
	if !strings.Contains(string(app), "buildRecordPriceLayer(config)") {
		t.Fatal("forward fix should keep the hardened call")
	}
}
