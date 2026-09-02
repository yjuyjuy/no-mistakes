// Package skill holds the canonical content of the no-mistakes agent skill.
//
// It is the single source of truth for the skill's identity (name and
// trigger description) and its SKILL.md body. The genskill tool renders
// Markdown() to the public skills/no-mistakes/SKILL.md (verified fresh in
// CI), and the init command installs the same rendering into the user-level
// agent skill directories under the user's home.
// The CLI's axi home view reuses Description so the two never drift.
package skill

import (
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/gateguidance"
	"github.com/kunchenguid/no-mistakes/internal/testguidance"
)

// Name is the skill directory name and frontmatter name. It must match the
// installed directory so the agent exposes it as the /no-mistakes command.
const Name = "no-mistakes"

// Description is the trigger-shaped frontmatter description: what the skill
// does and when to use it. It is the single most important field for the
// agent's decision to load the skill, so it leads with outcomes and keywords.
const Description = "Validate your code changes through the no-mistakes pipeline - automated code review, tests, lint, docs, push, PR, and CI - before they reach the configured push target. Use when the user asks to run no-mistakes, gate or ship or validate their changes, push safely, asks you to do a task and then validate it, or invokes /no-mistakes."

// Markdown returns the complete SKILL.md document (YAML frontmatter plus body).
// The output is deterministic so it can be regenerated and diff-checked. It is
// the single rendering: the canonical public skill (surfaced by discovery
// tools, e.g. `npx skills add kunchenguid/no-mistakes`) and the copy init
// installs at user level are identical. Older versions vendored a variant with
// `metadata.internal: true` into each target repo to keep the vendored copy
// out of repo skill listings; the user-level install is a genuine user
// installation that should stay discoverable, so no internal marker exists
// anymore.
func Markdown() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + Name + "\n")
	b.WriteString("description: " + Description + "\n")
	b.WriteString("user-invocable: true\n")
	b.WriteString("---\n")
	b.WriteString(body)
	return b.String()
}

// body is the Markdown instructions an agent reads when the skill activates.
// It follows the fleet's five-section skill template: a trigger description,
// when to reach for the tool, curated workflows, fleet conventions, and
// non-goals. Help-derivable content (flag lists, usage lines) is deliberately
// absent - the agent can run `--help` itself. Do not embed live state here:
// the skill is static.
const body = `
# no-mistakes

Validate committed changes on a feature branch through the local gate pipeline
(intent, rebase, review, test, document, lint, push, PR, CI) before they reach the
configured push target. You drive it through the ` + "`" + `no-mistakes axi` + "`" + ` command family,
which prints machine-readable [TOON](https://toonformat.dev) to stdout and progress
to stderr. Every subcommand takes ` + "`" + `--help` + "`" + `; this skill covers only what ` + "`" + `--help` + "`" + `
cannot know.

` + gateguidance.SkillBoundary + `

## When to reach for the pipeline

- Reach for it when a change is meant to ship: it is committed on a feature
  branch and you want it reviewed, tested, documented, pushed, and carried
  through PR and CI. That is the whole point of the tool.
- Do not reach for it to check a work in progress. Running your own tests or
  linter is minutes faster, and the pipeline validates committed history rather
  than your working tree, so uncommitted work is invisible to it.
- Do not reach for it inside an active run. A validation-step agent has its own
  phase and the outer executor owns everything else (see the boundary above).
- Prefer ` + "`" + `no-mistakes axi status` + "`" + ` over starting anything when you only need to
  know what a run is doing. Inspection never disturbs a run.
- Prerequisites the pipeline cannot supply for you: the work is committed, you
  are on a feature branch rather than the repository's default branch, the
  repository was set up with ` + "`" + `no-mistakes init` + "`" + `, and the daemon has a runnable
  configured pipeline agent - a supported native agent binary, the
  ` + "`" + `agent: cursor` + "`" + ` ACP alias, or an explicit ` + "`" + `acp:<target>` + "`" + ` through ` + "`" + `acpx` + "`" + `. You
  are the AXI driver, not an implicit pipeline-agent backend. When one of these
  is missing, ` + "`" + `axi run` + "`" + ` returns an ` + "`" + `error:` + "`" + ` naming the fix; ` + "`" + `no-mistakes doctor` + "`" + `
  reports a configuration or binary problem.

## Two ways to invoke

` + "`" + `/no-mistakes` + "`" + ` works in two modes, depending on whether the user hands you a
task along with the command:

- **Validate-only** - bare ` + "`" + `/no-mistakes` + "`" + ` (optionally with flag-style requests
  such as "skip the lint step", which you translate into the matching ` + "`" + `axi run` + "`" + `
  flags yourself). The user's code changes are already committed; validate them
  and report the outcome at the end.
- **Task-first** - ` + "`" + `/no-mistakes <task>` + "`" + `, e.g.
  ` + "`" + `/no-mistakes add a --json flag to the status command` + "`" + `. First carry out the
  task yourself, then validate the result through the pipeline:
  1. **Check scope.** Inspect ` + "`" + `git status` + "`" + ` before you change or commit anything.
     Preserve unrelated pre-existing uncommitted changes, and when you commit,
     commit only the changes that belong to the user's task.
  2. **Do the work.** Make the changes the task describes, then **commit them on
     a feature branch**. If the user is on the repository's default branch,
     create a feature branch first - the gate validates committed history on a
     non-default branch, so the work must land there before you run.
  3. **Then validate**, passing the user's task as your ` + "`" + `--intent` + "`" + `.

` + testguidance.Rule + `

## Workflows

### Orient before you start

` + "`" + "`" + "`" + `sh
no-mistakes axi                # home view: identity, branch, daemon state, recent runs, next steps
no-mistakes axi status         # current branch's run in full detail
` + "`" + "`" + "`" + `

An active run on your current branch: inspect it with ` + "`" + `no-mistakes axi status` + "`" + `,
and if it is parked at a gate, drive it with ` + "`" + `no-mistakes axi respond` + "`" + `.
Reattach an in-flight run by re-running ` + "`" + `no-mistakes axi run` + "`" + ` when it still matches your current ` + "`" + `HEAD` + "`" + ` - either as the submitted head or as the current pipeline head.
An active run on another branch is not yours: leave it alone and start your own
validation for the current branch.

### Drive a run to an outcome

` + "`" + "`" + "`" + `sh
no-mistakes axi run --intent "<what the user set out to accomplish>"
no-mistakes axi respond --action fix --findings <id1,id2> --instructions "<guidance>"
no-mistakes axi respond --action approve
` + "`" + "`" + "`" + `

` + "`" + `axi run` + "`" + ` and every ` + "`" + `axi respond` + "`" + ` block until the next ` + "`" + `gate:` + "`" + ` or the final
` + "`" + `outcome:` + "`" + `; review, test, and CI can each take several minutes, so allow a long
timeout rather than cancelling or re-issuing the call. Read every return: on a
` + "`" + `gate:` + "`" + `, decide and respond; loop until an ` + "`" + `outcome:` + "`" + `. The run never advances
past a gate on its own, so never idle-wait for it to move by itself. To watch
progress without disturbing it, run ` + "`" + `no-mistakes axi status` + "`" + ` from a separate
call.

The ` + "`" + `--intent` + "`" + ` you pass is what the user set out to accomplish, in their terms -
not a description of the diff. Err on the side of completeness: the review step
uses it to tell a deliberate decision apart from a mistake, so a thin one-line
summary makes it flag choices the user already made. Capture their goal, the
decisions and tradeoffs made along the way, constraints ruled in or out, and
anything they asked for that would look surprising in the diff. A few sentences
to a short paragraph is normal. Passing it directly is also faster and steadier
than letting no-mistakes infer it from local agent transcripts.

### Decide at a gate

A ` + "`" + `gate:` + "`" + ` object carries a ` + "`" + `findings` + "`" + ` table whose ` + "`" + `action` + "`" + ` column is the
pipeline's own classification, and that column decides who may answer:

- ` + "`" + `auto-fix` + "`" + ` - mechanical and low-risk; authorize it on your own judgment with
  ` + "`" + `--action fix` + "`" + `.
- ` + "`" + `no-op` + "`" + ` - informational; nothing to do.
- ` + "`" + `ask-user` + "`" + ` - a decision that belongs to the user. Stop and escalate (see
  Fleet conventions).

**Review auto-fix is disabled by default** (` + "`" + `auto_fix.review: 0` + "`" + `; a repo or
global ` + "`" + `auto_fix.review > 0` + "`" + ` override re-enables it), so blocking and
ask-user review findings park for your decision rather than being silently self-fixed.
Other steps such as test and lint may auto-fix within the pipeline and re-run
before they ever gate. When ` + "`" + `axi status` + "`" + ` shows ` + "`" + `awaiting_agent: parked
<duration>` + "`" + `, the run is waiting on your ` + "`" + `respond` + "`" + `; the field is observability
only and never auto-resumes anything. An ` + "`" + `active_steps` + "`" + ` table with
` + "`" + `last_activity` + "`" + ` prefixed ` + "`" + `quiet` + "`" + ` is a liveness clue, not permission to cancel,
rerun, or edit the worktree yourself.

Two ` + "`" + `respond` + "`" + ` flags matter beyond the action itself: ` + "`" + `--add-finding '<json>'` + "`" + `
folds a problem you spotted that the pipeline did not surface into the same fix
round, and ` + "`" + `--step <name>` + "`" + ` answers a step other than the active gate (rarely
needed).

### Read an outcome

- ` + "`" + `checks-passed` + "`" + ` - validated and CI is green (or the trusted default-branch
  config declares ` + "`" + `no_ci: true` + "`" + ` with no checks registered), but the PR is not
  merged. **You are done driving.** Tell the user the PR is ready and ask them
  to review and merge it; the PR link is in the ` + "`" + `help` + "`" + ` line. Do not wait for the
  merge and never treat "no CI checks reported" alone as green.
- ` + "`" + `passed` + "`" + ` - the change cleared the gate and the PR was merged or closed.
- ` + "`" + `failed` + "`" + ` or ` + "`" + `cancelled` + "`" + ` - fix what the output points at, commit on the same
  feature branch, then start a fresh ` + "`" + `no-mistakes axi run --intent "..."` + "`" + ` or
  ` + "`" + `no-mistakes rerun` + "`" + `. Never leave the user at a ` + "`" + `failed` + "`" + ` outcome without either
  retrying or explaining what blocks it.

On success, summarize what the pipeline validated and found. If the output has a
` + "`" + `fixes` + "`" + ` table, the pipeline fixed findings your change missed: list each one so
the user can review them.

### Inspect a specific run or step

` + "`" + "`" + "`" + `sh
no-mistakes axi status --run <id>                # a named run, including one on another branch
no-mistakes axi logs --step <name> --full        # the entire log of one step
no-mistakes axi abort --run <id>                 # cancel a specific run, even outside its worktree
` + "`" + "`" + "`" + `

` + "`" + `axi status` + "`" + ` is scoped to your current branch when ` + "`" + `--run` + "`" + ` is omitted: an
implicitly resolved ` + "`" + `run:` + "`" + ` is this branch's. A run under ` + "`" + `other_branch_run:` + "`" + ` is
one you named with ` + "`" + `--run <id>` + "`" + ` that belongs to another branch - never read its
status or outcome as your own work.
An explicit ` + "`" + `--run <id>` + "`" + ` rendered under ` + "`" + `run:` + "`" + ` while the current branch is unknown (detached ` + "`" + `HEAD` + "`" + ` or a branch-lookup failure) encodes no branch relationship.
In a successful status response, no run object at all means this branch has no
run yet, whatever the recent-runs table lists; an ` + "`" + `error:` + "`" + ` response proves
nothing about run ownership, so act on the error instead of concluding the
branch is idle.

### Synchronize the local branch after a run

` + "`" + "`" + "`" + `sh
no-mistakes axi sync --check      # freshly verify an offered plan
no-mistakes axi sync              # apply only an offered guarded synchronization
no-mistakes axi sync --recover    # take back custody of preserved pipeline commits
` + "`" + "`" + "`" + `

Before any post-pipeline local commit or fresh run, read the structured
` + "`" + `branch_sync` + "`" + ` object returned by AXI home, status, or a drive result, and follow
its ` + "`" + `next_action.code` + "`" + `:

- ` + "`" + `sync` + "`" + ` - run ` + "`" + `no-mistakes axi sync` + "`" + `. That guarded sync may be a strict
  fast-forward or a content-equivalent diverged advance that anchors the
  pre-sync head before moving the branch with reset semantics; genuine
  divergence stays blocked.
- ` + "`" + `continue_active_run` + "`" + ` - the pipeline still owns the branch. Run the reported
  command, keep driving the run, and make no local follow-up commits.
- ` + "`" + `recover_custody` + "`" + ` - a terminal run left unpublished pipeline commits
  preserved in the local gate. Run ` + "`" + `no-mistakes axi sync --recover` + "`" + ` to take
  the preserved head, or ` + "`" + `no-mistakes rerun` + "`" + ` to resume validating it.
  Recovery fast-forwards, or adopts a diverged preserved head proven to carry every local change (the
  ordinary result of the pipeline rebasing onto a newer base), after anchoring
  your pre-recovery head under ` + "`" + `refs/no-mistakes/recover-local/<run>` + "`" + `. That
  proof is deliberately narrow, so a rebase whose fix rounds also rewrote your
  own lines refuses instead: when nothing can tell a deliberate pipeline fix
  from a dropped change, the decision is yours. ` + "`" + `--keep-local` + "`" + ` keeps your
  current head while the preserved commits stay anchored under
  ` + "`" + `refs/no-mistakes/recover/<run>` + "`" + `.

A ` + "`" + `branch_sync.state` + "`" + ` of ` + "`" + `user_owned` + "`" + ` means the run went terminal
before changing the submitted head and cancellation released the branch: the exact
branch and head are yours and immediately usable, no sync action is needed, and
a repeated ` + "`" + `--recover` + "`" + ` there is a harmless no-op. When synchronization is
blocked, process that structured state instead of improvising
reset, stash, merge, rebase, force, or branch replacement.
Afterwards, commit the follow-up on top and re-run
` + "`" + `no-mistakes axi run --intent "..."` + "`" + ` with the original user intent.

## Fleet conventions

**The worker that started a run owns it end to end.** Every ` + "`" + `no-mistakes axi
run` + "`" + ` and ` + "`" + `no-mistakes axi respond` + "`" + ` call through to the next gate or the final
outcome belongs to that one driver. A second agent responding to the same gate,
or a human hand-editing the worktree mid-run, duplicates ownership and breaks
the gate flow.

**An ` + "`" + `ask-user` + "`" + ` finding is a real stop.** It is not a slow ` + "`" + `auto-fix` + "`" + `: the
pipeline raised it because the finding challenges the user's deliberate intent
or changes product behavior. Relay it to the user as the pipeline wrote it - its
` + "`" + `id` + "`" + `, ` + "`" + `file` + "`" + `, and full ` + "`" + `description` + "`" + ` verbatim, without paraphrase or a
pre-judged answer - and translate their reply into ` + "`" + `--action fix` + "`" + ` (their
guidance in ` + "`" + `--instructions` + "`" + `), ` + "`" + `--action approve` + "`" + `, or ` + "`" + `--action skip` + "`" + `. ` + "`" + `--yes` + "`" + `
resolves ` + "`" + `ask-user` + "`" + ` findings automatically, which is exactly why it is forbidden
as a way past one: use it only when the user has explicitly asked you to drive
the whole run unattended.

**While a run is active, the pipeline owns the code.** Never fix a finding by
editing files yourself; ` + "`" + `--action fix` + "`" + ` has the pipeline apply the fix and
re-review the result. For the same reason do not ` + "`" + `abort` + "`" + ` or ` + "`" + `rerun` + "`" + ` mid-run to
go fix something yourself, even a real bug of your own - that discards in-flight
work and forces full re-validation. ` + "`" + `abort` + "`" + ` and ` + "`" + `rerun` + "`" + ` are *between-runs*
actions, correct after a ` + "`" + `failed` + "`" + ` or ` + "`" + `cancelled` + "`" + ` outcome, never a way to
circumvent a gate.

**Follow-up work goes on top, never over.** Commit post-pipeline follow-up work
on top of the existing branch so every pipeline fix commit remains present.
Never abort-and-restart, reset, or replace the branch in a way that drops prior
gate-fix commits.

**A ` + "`" + `checks-passed` + "`" + ` PR keeps monitoring itself.** If this PR later falls behind
the default branch or hits a merge conflict, the CI monitor rebases onto the
base, resolves it, restarts validation at Review, and re-pushes it through Push
automatically - run no command and never hand-rebase. Only when that monitor is
no longer running (PR closed, run aborted, idle-timeout, or auto-fix exhausted)
recover with ` + "`" + `no-mistakes rerun` + "`" + `. Reaching for ` + "`" + `no-mistakes axi run` + "`" + ` on a still
active PR only reattaches to the monitor and returns its output without
rebasing.

**Output and exit-code conventions.** Output is TOON: ` + "`" + `key: value` + "`" + ` pairs,
` + "`" + `name[N]{cols}:` + "`" + ` tables, and ` + "`" + `help[N]:` + "`" + ` hints that tell you the next commands.
Errors print as ` + "`" + `error: ...` + "`" + ` on stdout with their own ` + "`" + `help` + "`" + ` list, so act on the
suggestion. Exit codes are ` + "`" + `0` + "`" + ` for success, no-ops, and normal decision gates,
` + "`" + `1` + "`" + ` for failed or cancelled final outcomes, and ` + "`" + `2` + "`" + ` for bad usage.

## Non-goals

- Not a replacement for your own fast feedback loop. Run your tests and linter
  directly while iterating; the pipeline is the shipping gate, not a compiler.
- Not a merge button. ` + "`" + `checks-passed` + "`" + ` is where your driving ends; a human merges.
- Not a flag reference. ` + "`" + `no-mistakes axi run --help` + "`" + ` and each subcommand's
  ` + "`" + `--help` + "`" + ` own the flags, defaults, and step names.
- Not usable on the default branch or on uncommitted work, by design.
- Not something to bypass. There is no supported way past an ` + "`" + `ask-user` + "`" + ` finding
  other than the user's decision, and no supported way to take a branch back
  from an active run.
`
