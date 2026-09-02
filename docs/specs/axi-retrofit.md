# AXI retrofit spec: no-mistakes

This is an audit document, not a change.
It records how the installed `no-mistakes` binary scores against the ten AXI principles, and the smallest set of changes that would bring it to the bar.
The retrofit itself is a separate ticket.

Audited binary: `/root/.local/bin/no-mistakes` -> `~/.no-mistakes/bin/no-mistakes`, version `v1.41.2-19-gd62ad33 (d62ad33) 2026-08-30T15:03:02Z`.
Principles source: `/work/toolings/axi/principles.yaml` and the AXI skill at `/work/toolings/axi/.agents/skills/axi/SKILL.md`.
Bar: all ten principles are mandatory, per the compliance decision in ticket 012 of the agentic-tooling-next map.

The audit was performed read-only against a shared daemon that other agents' validation pipelines depend on.
No command that starts, stops, restarts, updates, or reconfigures the daemon was run, and no pipeline run was started.
Consequences of that constraint are recorded in the Non-goals section.

The tool has two front doors, and the distinction runs through the whole scorecard.
`no-mistakes` is the human surface: Cobra help, a TTY wizard, and lipgloss-rendered panels.
`no-mistakes axi` is the agent surface: a TOON-emitting command family, explicitly documented as such by its own `--help`.
AXI principles are judged against the agent surface, since that is the surface an agent is instructed to use.
The human surface only enters the scorecard where it leaks into the agent surface, which is exactly what the update notice does.

## 1. Scorecard

### Principle 1 - Token-efficient output

**Pass.**

Every `axi` subcommand emits TOON on stdout.

```
$ cd /work/hyfin-server
$ no-mistakes axi
bin: ~/.no-mistakes/bin/no-mistakes
description: "Validate your code changes through the no-mistakes pipeline - automated code review, tests, lint, docs, push, PR, and CI - before they reach the configured push target. ..."
repo: /work/hyfin-server
current_branch: dev
daemon: running
count: 6 of 6 total
runs[6]{id,branch,status,head,pr}:
  "01M1J058V2ZT0K73ZR875HP0Z1",fm/scope-schedule-balance-divergence-alert,completed,a3b0572b,"https://bitbucket.org/dashnow/hyfin-server/pull-requests/2532"
  ...
```

The tabular array header `runs[6]{id,branch,status,head,pr}` is real TOON, not JSON, and the same shape appears in `axi status`, `axi logs`, `axi abort`, and `axi sync`.
Errors are TOON too (see Principle 6).
This principle is already met and needs no change.

### Principle 2 - Minimal default schemas

**Pass.**

The default list schema is five fields, `{id,branch,status,head,pr}`, which sits at the upper edge of the stated 3-4 guidance but is defensible: every field is a decision input for an agent choosing whether a run is its own, and none of them is long-form content.
The detail view adds one nested table.

```
$ no-mistakes axi status --run 01M1J058V2ZT0K73ZR875HP0Z1
  steps[9]{step,status,findings,duration_ms}:
    intent,completed,0,15
    review,completed,2,24650
    ...
```

Long-form content stays out of lists: finding descriptions and step logs are reachable only through `axi status` gate rendering and `axi logs`.
The home-view default limit is ten runs (`recentRunsHomeLimit`), which covered every repository inspected without a follow-up call.
No `--fields` flag exists, but with a five-field schema and no omitted cheap field an agent commonly needs, nothing is currently unreachable because of its absence.

### Principle 3 - Content truncation

**Pass.**

Three separate truncation sites were exercised, and each one states the total and names the escape hatch.

Step logs tail at 40 lines and disclose the full-log command:

```
$ no-mistakes axi logs --run 01M1G1KDMAEVR2VN0YMRAXYAY1 --step review
lines: 40 of 51 total (tail)
...
help[1]: Run `no-mistakes axi logs --run 01M1G1KDMAEVR2VN0YMRAXYAY1 --step review --full` to see the entire log
```

A short log reports `lines: 1 total` with no `--full` hint, which matches the rule that the escape hatch is offered only when content was actually cut:

```
$ no-mistakes axi logs --run 01M1J058V2ZT0K73ZR875HP0Z1 --step lint
step: lint
run: "01M1J058V2ZT0K73ZR875HP0Z1"
lines: 1 total
log[1]{line}:
  "lint assessed in the combined document+lint housekeeping pass: 0 unresolved items"
```

Finding descriptions and gate summaries truncate at 600 and 1200 characters with a `… (truncated, N chars total)` suffix.

One residual weakness is worth recording without calling it a gap.
A single log LINE is never truncated, only the number of lines is bounded, so a step whose agent emitted one very long JSON line returns that entire line.
The `test` step of run `01M1J058V2ZT0K73ZR875HP0Z1` reports `lines: 8 total` yet returns 3753 bytes, most of it inside one line.
The line count is therefore an unreliable proxy for response size, though the disclosure requirement itself is met.

### Principle 4 - Pre-computed aggregates

**Pass.**

Lists carry a total, not just a page size:

```
$ cd /work/firstmate-work/projects/hyfin-server
$ no-mistakes axi
count: 10 of 127 total
```

Derived summaries appear where a follow-up call would otherwise be needed.
`axi status` folds the findings roll-up (`findings: 2 info`, `findings: 2 awaiting`) and per-step finding counts and durations into one response, and the home view precomputes `daemon: running` so an agent never has to probe daemon liveness separately.
A terminal run also carries its own verdict inline:

```
$ no-mistakes axi status --run 01M1G1KDMAEVR2VN0YMRAXYAY1
outcome: failed
error: "step push failed: refusing to push: run has no durably recorded review-approved head"
```

### Principle 5 - Definitive empty states

**Pass.**

Zero states are stated, not implied by silence.
In an initialized repository that has never had a run:

```
$ cd /root/.treehouse/firstmate-work-468eb4/2/firstmate-work/projects/hyfin-server
$ no-mistakes axi
runs: 0 runs yet in this repository
$ no-mistakes axi status
current_branch: dev
runs_on_current_branch: 0
runs: 0 runs yet in this repository
```

A missing step log is also explicit rather than blank:

```
$ no-mistakes axi logs --run 01M1G1KDMAEVR2VN0YMRAXYAY1 --step ci
log: "no log recorded for step \"ci\" in this run"
```

`runs_on_current_branch: 0` alongside the repository-wide list is a particularly good form of this principle, because it separates "you have nothing" from "the repository has nothing".

### Principle 6 - Structured errors and exit codes

**Gap.**

Three of the four sub-requirements pass, and the fourth fails in a way that is exactly the failure mode the principle warns about.

Idempotent mutations pass, including the harder unknown-id case:

```
$ no-mistakes axi abort --run 01M1HHBJAJ59W0YWP0VY97MJK2
aborted: false
run: "01M1HHBJAJ59W0YWP0VY97MJK2"
run_status: cancelled
detail: run is already terminal (idempotent no-op)     # exit 0

$ no-mistakes axi abort --run NOSUCHRUN
aborted: false
run: NOSUCHRUN
detail: no run with that id exists (no-op)             # exit 0
```

Structured errors on stdout pass, with a correct usage-versus-error exit split:

```
$ no-mistakes axi respond --action approve
error: no active run to respond to
help[1]: "Run `no-mistakes axi run --intent \"...\"` to start one"      # exit 1

$ no-mistakes axi respond --action bogus
error: "unknown action \"bogus\""
help[1]: "Valid actions: approve, fix, skip"                            # exit 2

$ no-mistakes axi logs --run 01M1J058V2ZT0K73ZR875HP0Z1
error: "--step is required"
help[1]: "Valid steps: intent, rebase, review, test, document, lint, push, pr, ci"   # exit 2
```

No interactive prompts pass on the agent surface: `axi run --help` documents the family as "driven entirely by flags (no interactive prompts)", and every command exercised returned without reading stdin.

Failing loud on unrecognized input is where it breaks.
An unknown flag is rejected, but by Cobra, on stderr, unstructured, with no valid-flag list and the wrong exit code:

```
$ no-mistakes axi status --stat closed
                                              # stdout: empty
unknown flag: --stat                          # stderr, exit 1

$ no-mistakes axi run --intnet x
                                              # stdout: empty
unknown flag: --intnet                        # stderr, exit 1
```

An agent reading only stdout, which is what the tool's own channel contract tells it to do, sees an empty response with a non-zero status and no reason.
The principle requires this to land on stdout as a TOON error, with exit code 2 and the command's valid flags inline.
The same defect applies to an unknown subcommand (`no-mistakes axi bogus` prints `unknown command "bogus" for "no-mistakes axi"` on stderr with exit 1) and to a flag given without its argument (`no-mistakes axi status --run` prints `flag needs an argument: --run` on stderr with exit 1).
Compare the tool's own hand-written validators above, which already do this correctly, so the gap is confined to the arguments Cobra rejects before the command body runs.

The output-channels rule is also violated, but by a separate mechanism, and it is the one this ticket calls out by name.
Every invocation of the agent surface emits the update notice on stderr:

```
$ no-mistakes axi 2>&1 1>/dev/null
A new version of no-mistakes is available: v1.41.2-19-gd62ad33 -> v1.60.2
Run "no-mistakes update" to update
```

**The update-notice classification, decided.**
The ticket asks whether this output is ambient context (Principle 7) or noise.
It is neither exactly, and the correct classification is that it is a Principle 6 output-channel question that resolves in the notice's favour, with one condition attached.

It is NOT ambient context.
Principle 7 ambient context is state relevant to the work, injected once at session start through an opt-in integration, so the agent can act on it.
This notice is emitted on every single invocation, carries no information about the repository, the branch, or any run, and its only suggested action, `no-mistakes update`, is one the fleet actively forbids: this box runs a shared daemon serving other agents' pipelines, and updating it would kill their in-flight runs.
An agent that obeyed the notice would cause an incident.

It is also NOT noise that must be removed.
It goes to stderr, and Principle 6 explicitly designates stderr as the channel agents do not read, which is precisely so operator-facing diagnostics have somewhere to go.
Stdout, the channel the agent parses, was verified byte-clean on every command exercised: the notice never appears there, including in the empty-stdout unknown-flag cases above.
Deleting an operator's upgrade signal to satisfy an agent-facing standard would be solving the problem on the wrong surface.
The inventory note that scored this tool ("top-level help prints update nag noise") describes the HUMAN surface, where a colour-coded nag above `--help` is a real ergonomic wart, but the human surface is not what AXI judges.

The condition, and the only change this classification demands, is discoverable suppression.
The notice is already suppressible with `NO_MISTAKES_NO_UPDATE_CHECK=1`, verified:

```
$ NO_MISTAKES_NO_UPDATE_CHECK=1 no-mistakes axi 2>&1 1>/dev/null
                                              # empty
```

But that variable is documented only in the docs site, not in `axi --help` or in the installed skill, so an agent harness that captures merged output has no in-band way to learn it exists.
The retrofit therefore names the variable at the point of use rather than removing the notice.

### Principle 7 - Ambient context

**Gap.**

The skill half is present and good.
`no-mistakes init` installs the skill at user level into both `~/.claude/skills/no-mistakes/SKILL.md` and `~/.agents/skills/no-mistakes/SKILL.md`, both of which exist on this box, so Claude Code, Codex, OpenCode, Rovo Dev, and Pi all reach it.
The skill is generated from a Go source constant with a `make lint` drift check, which is the single-source-of-truth rule satisfied more strictly than the principle asks.
Its frontmatter is trigger-shaped, with `user-invocable: true` and an outcome-focused description.

The hook half is entirely absent.
Nothing in the codebase registers a `SessionStart` hook, and the installed environment confirms it: `~/.claude/settings.json` contains no `no-mistakes` reference, `~/.codex/hooks.json` does not exist, and `~/.config/opencode/plugins/` does not exist.
The principle names the session hook as the PRIMARY integration and the skill as the secondary one, so the tool ships only the fallback.
This matters more here than for a read-only tool: the home view already computes exactly the compact dashboard a `SessionStart` hook wants, so an agent that started a session inside a repository with a parked gate would learn about it immediately instead of only when it thought to ask.

### Principle 8 - Content first

**Gap, on the surface where it counts.**

`no-mistakes axi` with no subcommand is exemplary content-first: identity, repository, branch, daemon state, live runs, next steps, no usage manual.
That is the surface the skill tells agents to use.

The gap is that bare `no-mistakes`, which is what a probing agent runs first, does not reach that surface, and in two of three situations returns something worse than help text.
In an uninitialized repository, stdout is empty:

```
$ cd <uninitialized repo>
$ no-mistakes                                # exit 1
                                             # stdout: EMPTY
repo not initialized (run 'no-mistakes init' first)     # stderr
```

The same condition through the agent surface is correct, which shows the fix is a routing question, not a missing capability:

```
$ no-mistakes axi                            # exit 1
error: repo not initialized (run 'no-mistakes init' first)
help[1]: Run `no-mistakes init` to set up the gate in this repository
```

In an initialized repository without a TTY, bare `no-mistakes` returns a lipgloss-rendered human panel, which is live data but not machine-readable:

```
$ cd /work/hyfin-server
$ no-mistakes </dev/null                     # exit 0
  No active run.

  Recent runs
  completed    fm/scope-schedule-balance-divergence-alert a3b0572b  32 mins ago  https://...
  ...
  Start a new pipeline:
  git push no-mistakes <branch>
```

In a TTY it can enter an interactive setup wizard instead, which no agent can complete.
The `axi` prefix is a reasonable design for a tool with a genuine human surface, so the fix is not to make bare `no-mistakes` emit TOON; it is that a non-TTY caller must be routed to, or at least pointed at, the agent surface rather than shown a panel or given an empty stdout.

### Principle 9 - Contextual disclosure

**Pass.**

Every list and mutation response carries `help[N]`, and the suggestions are relevant, actionable, parameterized, and situational rather than a fixed script.

The home view suggests starting a run with a placeholder rather than a guessed value:

```
help[4]: "Run `no-mistakes axi run --intent \"<what the user set out to accomplish>\"` to validate your changes", ...
```

The suggestions change with the state.
With runs present but none on this branch, the tool volunteers the correct disambiguation:

```
help[2]: "Run no-mistakes axi run --intent \"the user's goal\" --yes to validate the current branch",
         No run exists for this branch; every run listed above is on another branch -
         inspect one deliberately with `no-mistakes axi status --run <id>`
```

Errors resolve to a specific command rather than "see --help", as shown under Principle 6.
Detail views correctly omit suggestions when self-contained: `axi status --run <id>` on a completed run ends at `outcome: passed` with no help block.
A truncated list discloses its own continuation (`Run ... --full to see the entire log`).
The one thing the help blocks carry beyond next steps is standing policy prose, notably the "commit post-pipeline follow-up work on top of the existing branch" line that appears on nearly every response; that is deliberate guardrail guidance rather than a next step, and it costs tokens on every call, but it does not violate the principle.

### Principle 10 - Consistent way to get help

**Gap, narrowly.**

The home view identifies the tool exactly as specified, with the home directory collapsed to `~` and a one-sentence description:

```
bin: ~/.no-mistakes/bin/no-mistakes
description: "Validate your code changes through the no-mistakes pipeline - automated code review, tests, lint, docs, push, PR, and CI - before they reach the configured push target. ..."
```

Per-subcommand `--help` is concise and complete, listing flags with defaults, marking required arguments, and staying scoped to the requested subcommand.
`axi logs --help` is 10 lines, `axi status --help` is 8, and `axi run --help` is 25 with the flag semantics spelled out.
None of them dumps the whole CLI manual.
The one shortfall against the principle's letter is that the `--help` blocks carry no usage examples, where the principle asks for two or three.

The version fast path is fast but incomplete.
`--version` and `-v` both print the bare version and exit 0, and both are explicitly exempted from the update check and its background refresh, so they stay side-effect-free probes.
Measured over five runs, `--version` costs 3.0 ms against a 0.4 ms `/bin/true` process floor in the same harness, so there is no ESM-style eager-graph problem to fix; by contrast a real `axi` call costs about 40 ms.
But `-V` is not accepted:

```
$ no-mistakes -V
unknown shorthand flag: 'V' in -V            # stderr, exit 1
```

The principle requires all three of `-v`, `-V`, and `--version`.
A harness probing with `-V` gets a failure indistinguishable from "not installed".

## 2. Change list

1. Route Cobra's own argument rejections through the AXI error renderer, so an unknown flag, an unknown `axi` subcommand, and a flag missing its argument all emit a TOON `error:` plus `help:` on stdout with exit code 2, and the unknown-flag case lists the subcommand's valid flags inline. (Principle 6)
2. Accept `-V` as a third spelling of the version flag, alongside the existing `-v` and `--version`, on the same side-effect-free fast path. (Principle 10)
3. Name `NO_MISTAKES_NO_UPDATE_CHECK=1` in the `axi --help` text and in the generated skill, as the supported way to silence the stderr update notice, and keep the notice itself on stderr. (Principle 6, and the decision recorded under it)
4. Give bare `no-mistakes` a non-TTY path that reaches the agent surface instead of the human panel or an empty stdout, including in an uninitialized repository, where the `axi` surface already returns the correct structured error. (Principle 8)
5. Ship a `SessionStart` integration for Claude Code, Codex, and OpenCode, installed only from an explicit opt-in setup command, idempotent, path-repairing, directory-scoped, and rendering the existing home view. (Principle 7)
6. Add two or three usage examples to each `axi` subcommand's `--help`. (Principle 10)

## 3. Non-goals

Consciously waived, with reasons.

**The human surface is not being converted.**
Bare `no-mistakes` in a TTY keeps its rendered panels and its setup wizard, and `no-mistakes --help` keeps its Cobra output including the coloured update notice above it.
AXI judges the agent surface, `no-mistakes` has real human users, and change 4 gives the non-TTY caller a machine-readable path without taking the human one away.

**The update notice is not being removed or moved to stdout.**
Decided under Principle 6 above: it is operator-facing output on the channel the standard reserves for operator-facing output, and stdout was verified clean.
Only its suppression switch becomes discoverable, per change 3.

**No `--fields` flag.**
Principle 2 offers it as a mechanism for schemas that had to omit fields.
The default schema here is five fields with nothing cheap and commonly needed left out, so adding the flag would add surface without removing a round trip.

**Per-line truncation of step logs is not being added.**
Recorded as a residual weakness under Principle 3 rather than a change, because the disclosure requirement is met and a line-length cap risks cutting a structured findings payload an agent needs whole. If large single-line responses become a measured problem, that is its own ticket with its own evidence.

**Standing guardrail prose in `help[N]` stays.**
It is a per-call token cost and is not a next-step suggestion, but it encodes a real data-loss guardrail, and trimming it is a safety judgement for the tool's owner rather than an AXI compliance change.

**Not exercised, and why: everything requiring a live pipeline run.**
A shared daemon on this box is serving other agents' validation pipelines, and the task's hard safety constraint forbids stopping, restarting, updating, or reconfiguring it.
So `axi run`, `axi respond` against a real gate, and `axi sync --recover` were audited only through their `--help` output, their source, and the records of runs other agents had already completed.
Gate rendering (Principle 9's mutation-response half) was read from `gateFieldsWithHelp` in `internal/cli/axi_render.go` rather than captured live.
The audit also did not exercise the interactive TTY wizard path of bare `no-mistakes`, since the audit ran without a TTY, which is the condition an agent runs under anyway.
This is a coverage limit of the audit, not a waiver of any principle: every verdict above rests on a transcript that was actually captured.

## 4. Evidence

### Bare invocation

Captured in `/work/hyfin-server`, a repository with the gate initialized and six recorded runs, on branch `dev`.
The bare invocation of the agent surface is `no-mistakes axi`; the bare invocation of the top-level binary is shown after it, because Principle 8's gap lives there.

```
$ cd /work/hyfin-server
$ no-mistakes axi
--- stdout ---
bin: ~/.no-mistakes/bin/no-mistakes
description: "Validate your code changes through the no-mistakes pipeline - automated code review, tests, lint, docs, push, PR, and CI - before they reach the configured push target. Use when the user asks to run no-mistakes, gate or ship or validate their changes, push safely, asks you to do a task and then validate it, or invokes /no-mistakes."
repo: /work/hyfin-server
current_branch: dev
daemon: running
count: 6 of 6 total
runs[6]{id,branch,status,head,pr}:
  "01M1J058V2ZT0K73ZR875HP0Z1",fm/scope-schedule-balance-divergence-alert,completed,a3b0572b,"https://bitbucket.org/dashnow/hyfin-server/pull-requests/2532"
  "01M1HHHX367ZVZ9675GF7VXHHM",fix/multi-partial-per-record-consume,completed,4b711439,"https://bitbucket.org/dashnow/hyfin-server/pull-requests/2524"
  "01M1HHBJAJ59W0YWP0VY97MJK2",fix/multi-partial-per-record-consume,cancelled,23549dd4,""
  "01M1HH0HPY4NENJM98QWW3W01C",fix/multi-partial-per-record-consume,cancelled,23549dd4,""
  "01M1G2CHEBDY8QMVEVXC3MBN6S",fix/multi-partial-per-record-consume,cancelled,a5bc93dc,"https://bitbucket.org/dashnow/hyfin-server/pull-requests/2524"
  "01M1G1HNZCXSW73BK9WMMMKT5W",fix/multi-partial-per-record-consume,completed,a5bc93dc,""
help[4]: "Run `no-mistakes axi run --intent \"<what the user set out to accomplish>\"` to validate your changes","Commit post-pipeline follow-up work on top of the existing branch so every pipeline fix commit remains present. Never abort-and-restart, reset, or replace the branch in a way that drops prior gate-fix commits.",The calling agent drives AXI gates but does not replace the configured pipeline agent; run `no-mistakes doctor` if no native agent or ACP runner is available,"How to drive the pipeline: `no-mistakes axi run --help`, or the `/no-mistakes` skill (loaded when you invoke `/no-mistakes`)"
--- exit: 0 ---
--- stderr ---
A new version of no-mistakes is available: v1.41.2-19-gd62ad33 -> v1.60.2
Run "no-mistakes update" to update
```

```
$ cd /work/hyfin-server
$ no-mistakes </dev/null
--- stdout ---
  No active run.

  Recent runs
  completed    fm/scope-schedule-balance-divergence-alert a3b0572b  32 mins ago  https://bitbucket.org/dashnow/hyfin-server/pull-requests/2532
  completed    fix/multi-partial-per-record-consume 4b711439  4 hours ago  https://bitbucket.org/dashnow/hyfin-server/pull-requests/2524
  cancelled    fix/multi-partial-per-record-consume 23549dd4  4 hours ago
  cancelled    fix/multi-partial-per-record-consume 23549dd4  4 hours ago
  cancelled    fix/multi-partial-per-record-consume a5bc93dc  18 hours ago  https://bitbucket.org/dashnow/hyfin-server/pull-requests/2524
  (1 more - run 'no-mistakes runs' to see all)

  Start a new pipeline:
  git push no-mistakes <branch>
--- exit: 0 ---
--- stderr ---
A new version of no-mistakes is available: v1.41.2-19-gd62ad33 -> v1.60.2
Run "no-mistakes update" to update
```

### Hot-path invocation

Inspecting a specific completed run is the hottest read an agent performs after starting one: it is how the agent learns whether the pipeline passed, which step produced findings, and where a failure landed.
Captured from `dev`, deliberately selecting a run that belongs to another branch, which is why the payload arrives under `other_branch_run:` rather than `run:`.

```
$ cd /work/hyfin-server
$ no-mistakes axi status --run 01M1J058V2ZT0K73ZR875HP0Z1
--- stdout ---
current_branch: dev
other_branch_run:
  id: "01M1J058V2ZT0K73ZR875HP0Z1"
  branch: fm/scope-schedule-balance-divergence-alert
  status: completed
  head: a3b0572b
  pr: "https://bitbucket.org/dashnow/hyfin-server/pull-requests/2532"
  findings: 2 info
  steps[9]{step,status,findings,duration_ms}:
    intent,completed,0,15
    rebase,completed,0,1258
    review,completed,2,24650
    test,completed,0,99944
    document,completed,0,45455
    lint,completed,0,19
    push,completed,0,3794
    pr,completed,0,12544
    ci,completed,0,253843
outcome: passed
--- exit: 0 ---
--- stderr ---
A new version of no-mistakes is available: v1.41.2-19-gd62ad33 -> v1.60.2
Run "no-mistakes update" to update
```
