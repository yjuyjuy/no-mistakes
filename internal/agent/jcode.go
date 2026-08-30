package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/kunchenguid/no-mistakes/internal/shellenv"
)

// jcodeAgent spawns the jcode CLI for each invocation. jcode runs a single
// message non-interactively with `jcode run --ndjson --quiet <prompt>`,
// emitting newline-delimited JSON events on stdout. Its argv is rebuilt on
// every Run, so per-step model and effort overrides
// (agent_args_override_per_step) take effect exactly like the claude and codex
// adapters. jcode is NOT driven through the ACP bridge, which cannot express
// per-invocation flags.
//
// Effort: `jcode run` has no --effort flag (unlike the interactive /effort),
// so the adapter accepts a no-mistakes-managed `--effort <level>` pseudo-flag
// in the operator's agent_args_override[_per_step].jcode args, translates it
// into the JCODE_*_REASONING_EFFORT environment variables jcode reads for
// reasoning depth, and keeps it out of argv where `jcode run` would reject it.
// When no effort is pinned, invocations default to "low", matching the old
// claude-era pipeline override (agent_args_override.claude: --effort low) so
// jcode-driven validation steps stay cheap on simple tickets.

type jcodeAgent struct {
	bin       string
	extraArgs []string
	// disableProjectSettings is the resolved, trusted-only opt-out. When true,
	// runOnce suppresses AGENTS.md loading via JCODE_NO_AGENTS_MD.
	disableProjectSettings bool
	subprocessContext
}

// jcodeNoAgentsMDEnv is the environment entry that suppresses AGENTS.md
// loading in jcode: JCODE_NO_AGENTS_MD skips both the project ./AGENTS.md and
// the global ~/AGENTS.md before any path check (jcode PR #37,
// crates/jcode-base/src/prompt.rs). jcode run has no CLI flag for this knob,
// so the environment is the only surface.
const jcodeNoAgentsMDEnv = "JCODE_NO_AGENTS_MD=1"

// jcodeEffortFlag is the accepted effort pseudo-flag inside
// agent_args_override[_per_step].jcode.
const jcodeEffortFlag = "--effort"

// jcodeDefaultEffort is the reasoning effort applied when the operator does
// not pin one. It restores the old claude-era pipeline behavior (every
// validation step at low effort); operators can raise or lower it per step via
// `--effort <level>` (none, minimal, low, medium, high, xhigh, max, or
// "default" to fall back to jcode's model default).
const jcodeDefaultEffort = "low"

// jcodeEffortLevel returns the operator-pinned effort level from extraArgs
// (`--effort <level>` or `--effort=<level>`, last occurrence wins), or "" when
// no effort is pinned. The level is passed through untranslated: jcode
// validates and clamps it per model.
func jcodeEffortLevel(extraArgs []string) string {
	level := ""
	for i := 0; i < len(extraArgs); i++ {
		switch {
		case extraArgs[i] == jcodeEffortFlag:
			if i+1 < len(extraArgs) {
				level = extraArgs[i+1]
				i++
			}
		case strings.HasPrefix(extraArgs[i], jcodeEffortFlag+"="):
			level = strings.TrimPrefix(extraArgs[i], jcodeEffortFlag+"=")
		}
	}
	return level
}

// jcodeStrippedArgs returns extraArgs without the effort pseudo-flag and its
// value. buildArgs must never put them in argv: `jcode run` rejects unknown
// flags, and effort travels to jcode through the environment instead.
func jcodeStrippedArgs(extraArgs []string) []string {
	out := make([]string, 0, len(extraArgs))
	for i := 0; i < len(extraArgs); i++ {
		if extraArgs[i] == jcodeEffortFlag {
			if i+1 < len(extraArgs) {
				i++ // skip the level token; it is not an argv flag either
			}
			continue
		}
		if strings.HasPrefix(extraArgs[i], jcodeEffortFlag+"=") {
			continue
		}
		out = append(out, extraArgs[i])
	}
	return out
}

// jcodeEffectiveEffort resolves the effort level for one invocation: the
// operator-pinned --effort level, or the pipeline default.
func jcodeEffectiveEffort(extraArgs []string) string {
	if level := jcodeEffortLevel(extraArgs); level != "" {
		return level
	}
	return jcodeDefaultEffort
}

// jcodeEffortEnv returns the JCODE reasoning-effort environment entries for
// level. jcode reads reasoning effort per provider family (Anthropic and
// OpenAI), and these are the only non-interactive knobs its `run` subcommand
// honors; providers without an effort axis ignore them.
func jcodeEffortEnv(level string) []string {
	return []string{
		"JCODE_ANTHROPIC_REASONING_EFFORT=" + level,
		"JCODE_OPENAI_REASONING_EFFORT=" + level,
	}
}

func (a *jcodeAgent) Name() string { return "jcode" }

// SupportsSessionResume reports jcode's native durable-session capability:
// every `run` result carries a session_id, and `jcode run --resume <id>`
// continues that session, so the review loop can keep role-isolated reviewer
// and fixer sessions.
func (a *jcodeAgent) SupportsSessionResume() bool { return true }

func (a *jcodeAgent) ReportsAgentAttempts() bool { return true }

// NeutralizesGateInstructions reports whether jcode is currently launched with
// the target repo's project agent-instruction files suppressed. It is
// meaningful only under the opt-out (disableProjectSettings): the gate only
// consults it when the repo opted out. runOnce appends JCODE_NO_AGENTS_MD=1 to
// the launch env whenever the opt-out is on, so jcode loads neither the
// checkout's ./AGENTS.md nor the global ~/AGENTS.md (jcode PR #37,
// crates/jcode-base/src/prompt.rs). jcode run has no CLI flag that re-enables
// AGENTS.md loading - the env knob is the only surface - so an operator
// agent_args_override cannot defeat it, and the entry is appended after every
// invocation env entry, so not even JCODE_NO_AGENTS_MD=0 in the environment
// can. Return true only while the opt-out is on, exactly like claude.
func (a *jcodeAgent) NeutralizesGateInstructions() bool {
	return a.disableProjectSettings
}

func (a *jcodeAgent) Run(ctx context.Context, opts RunOpts) (*Result, error) {
	return runWithRetry(ctx, "jcode", opts, claudeMaxRetries, classifyTransient, nil, func() (*Result, error) {
		return a.runOnce(ctx, opts)
	})
}

// withStepArgs returns the adapter to build this invocation's argv from: the
// receiver, or a copy carrying the per-step arg profile (see
// RunOpts.StepArgsOverride). See the claude adapter's withStepArgs for why a
// copy is used instead of a buildArgs parameter.
func (a *jcodeAgent) withStepArgs(opts RunOpts) *jcodeAgent {
	args := opts.resolveExtraArgs("jcode", a.extraArgs)
	if sameArgs(args, a.extraArgs) {
		return a
	}
	next := *a
	next.extraArgs = args
	return &next
}

func (a *jcodeAgent) runOnce(ctx context.Context, opts RunOpts) (*Result, error) {
	a = a.withStepArgs(opts)
	resumeID := ""
	if opts.Session != nil {
		resumeID = opts.Session.ID
	}
	prompt := buildJcodePrompt(opts.Prompt, opts.JSONSchema)
	args := a.buildArgs(prompt, resumeID)
	cmd := exec.CommandContext(ctx, a.bin, args...)
	cmd.Dir = opts.CWD
	cmd.Stdin = nil
	// Thread the invocation-scoped env entries AND the resolved reasoning
	// effort. The effort entries are appended last so the pipeline's effort
	// policy (--effort pin, else the low default) wins over any invocation
	// entry, mirroring how the old claude override placed `--effort low` ahead
	// of everything claude itself decided.
	env := append(append([]string(nil), opts.Env...), jcodeEffortEnv(jcodeEffectiveEffort(a.extraArgs))...)
	// Under the trusted disable_project_settings opt-out, suppress AGENTS.md
	// loading the way claude drops its project setting sources: the gate agent
	// must never load the target repo's ./AGENTS.md. Appended after every
	// invocation env entry so its value always wins.
	if a.disableProjectSettings {
		env = append(env, jcodeNoAgentsMDEnv)
	}
	cmd.Env = a.gitSafeEnv(opts.CWD, env)
	shellenv.ConfigureShellCommand(cmd)

	var stderrBuf []byte
	var stderrWG sync.WaitGroup
	started, err := startNativeAgentCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("jcode start: %w", err)
	}
	defer started.closePipes()
	pid := started.pid()
	emitAgentStarted(opts, "jcode", pid)

	stderrWG.Add(1)
	go func() {
		defer stderrWG.Done()
		stderrBuf, _ = io.ReadAll(started.stderr)
	}()

	result := &jcodeResult{}
	if err := parseJcodeEvents(ctx, started.stdout, opts.OnChunk, result); err != nil {
		err = started.waitAfterParseError(err)
		stderrWG.Wait()
		retErr := fmt.Errorf("jcode parse events: %w", err)
		emitAgentExited(opts, "jcode", pid, retErr)
		return nil, retErr
	}

	waitErr := started.wait()
	stderrWG.Wait()
	if waitErr != nil {
		detail := jcodeErrorDetail(result.errMessage, string(stderrBuf))
		if detail != "" {
			retErr := fmt.Errorf("jcode exited: %w: %s", waitErr, detail)
			emitAgentExited(opts, "jcode", pid, retErr)
			return nil, retErr
		}
		retErr := fmt.Errorf("jcode exited: %w", waitErr)
		emitAgentExited(opts, "jcode", pid, retErr)
		return nil, retErr
	}

	if result.errMessage != "" {
		retErr := fmt.Errorf("jcode reported error: %s", result.errMessage)
		emitAgentExited(opts, "jcode", pid, retErr)
		return nil, retErr
	}

	res, err := finalizeTextResult("jcode", result.text, opts.JSONSchema, result.usage)
	if res != nil {
		res.SessionID = result.sessionID
		res.Resumed = resumeID != ""
		res.Model = result.model
		// jcode's ndjson usage is per-invocation (each `tokens` event is one
		// model round of this run), so SessionUsageCumulative stays false and
		// per-round deltas equal the summed counters. Cache-creation cost is
		// surfaced per turn, so mark it meaningful.
		res.CacheCreationReported = res.UsageReported
		res.ModelProvider = normalizeJcodeProvider(result.provider)
	}
	emitAgentExited(opts, "jcode", pid, err)
	return res, err
}

func (a *jcodeAgent) Close() error { return nil }

// buildArgs constructs the jcode CLI arguments. User-supplied extraArgs (from
// agent_args_override / agent_args_override_per_step) are inserted between the
// `run` subcommand and the managed flags, so operator flags such as
// `-m <model>` take effect. A non-empty resumeID continues that session via
// `--resume <id>`. The prompt is always the final positional, delivered after a
// `--` separator so a prompt beginning with a dash is never parsed as a flag.
func (a *jcodeAgent) buildArgs(prompt, resumeID string) []string {
	extraArgs := jcodeStrippedArgs(a.extraArgs)
	args := make([]string, 0, len(extraArgs)+8)
	args = append(args, "run")
	args = append(args, extraArgs...)
	args = append(args, "--ndjson", "--quiet", "--no-update", "--no-selfdev")
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}
	args = append(args, "--", prompt)
	return args
}

// buildJcodePrompt appends a JSON-output contract to the user prompt when a
// schema is provided. jcode's `run` has no equivalent of codex's
// --output-schema flag, so the schema is inlined in the prompt the same way pi
// and copilot do, then the final assistant text is parsed with
// finalizeTextResult.
func buildJcodePrompt(prompt string, schema json.RawMessage) string {
	if len(schema) == 0 {
		return prompt
	}
	pretty, err := json.MarshalIndent(json.RawMessage(schema), "", "  ")
	if err != nil {
		pretty = []byte(schema)
	}
	return prompt + "\n\n## no-mistakes final output contract\n\n" +
		"When the task is complete, your final assistant response must be only valid JSON matching this JSON Schema. " +
		"Do not wrap it in Markdown fences. Do not include prose before or after the JSON object.\n\n" +
		string(pretty)
}

func jcodeErrorDetail(jcodeErr, stderr string) string {
	detail := strings.TrimSpace(jcodeErr)
	stderr = strings.TrimSpace(stderr)
	if detail != "" && stderr != "" {
		return detail + "; " + stderr
	}
	if detail != "" {
		return detail
	}
	return stderr
}

// normalizeJcodeProvider lowercases the provider label jcode reports (e.g.
// "Claude") so it matches the other adapters' provider strings ("anthropic",
// "openai"). jcode uses subscription-brand names, so this is a best-effort map
// used only for instrumentation.
func normalizeJcodeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "":
		return ""
	case "claude", "anthropic":
		return "anthropic"
	case "openai", "chatgpt":
		return "openai"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

// jcodeResult captures the parsed state of one jcode run.
type jcodeResult struct {
	text       string
	sessionID  string
	model      string
	provider   string
	usage      TokenUsage
	errMessage string
}

// jcodeEvent is the top-level ndjson event from `jcode run --ndjson`.
type jcodeEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`
	Provider  string `json:"provider,omitempty"`

	// text_delta / done
	Text string `json:"text,omitempty"`

	// error
	Message string `json:"message,omitempty"`

	// tokens (per model round)
	Input              *int `json:"input,omitempty"`
	Output             *int `json:"output,omitempty"`
	CacheReadInput     *int `json:"cache_read_input,omitempty"`
	CacheCreationInput *int `json:"cache_creation_input,omitempty"`
}

// parseJcodeEvents reads ndjson from the reader and dispatches events. It
// streams text_delta content to onChunk, captures the durable session identity
// and model/provider from the start/done events, accumulates the per-round
// `tokens` usage, and records the terminal text from the final `done` event
// (falling back to accumulated deltas).
func parseJcodeEvents(ctx context.Context, r io.Reader, onChunk func(string), result *jcodeResult) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), claudeScannerMaxTokenSize)

	var deltaText strings.Builder
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event jcodeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue // skip malformed lines
		}
		if event.SessionID != "" {
			result.sessionID = event.SessionID
		}
		if event.Model != "" {
			result.model = event.Model
		}
		if event.Provider != "" {
			result.provider = event.Provider
		}

		switch event.Type {
		case "text_delta":
			if event.Text != "" {
				deltaText.WriteString(event.Text)
				if onChunk != nil {
					onChunk(event.Text)
				}
			}
		case "tokens":
			result.usage.Add(TokenUsage{
				InputTokens:           intOrZero(event.Input),
				OutputTokens:          intOrZero(event.Output),
				CacheReadTokens:       intOrZero(event.CacheReadInput),
				CacheCreationTokens:   intOrZero(event.CacheCreationInput),
				Reported:              true,
				CacheCreationReported: true,
			})
		case "error":
			if event.Message != "" {
				result.errMessage = event.Message
			}
		case "done":
			// The done event carries the authoritative full text of the turn.
			if event.Text != "" {
				result.text = event.Text
			}
		}
	}

	if result.text == "" {
		result.text = deltaText.String()
	}
	return scanner.Err()
}

func intOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
