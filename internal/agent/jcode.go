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
// every Run, so per-step model overrides (agent_args_override_per_step) take
// effect exactly like the claude and codex adapters. jcode is NOT driven
// through the ACP bridge, which cannot express per-invocation flags.
type jcodeAgent struct {
	bin       string
	extraArgs []string
}

func (a *jcodeAgent) Name() string { return "jcode" }

// SupportsSessionResume reports jcode's native durable-session capability:
// every `run` result carries a session_id, and `jcode run --resume <id>`
// continues that session, so the review loop can keep role-isolated reviewer
// and fixer sessions.
func (a *jcodeAgent) SupportsSessionResume() bool { return true }

func (a *jcodeAgent) ReportsAgentAttempts() bool { return true }

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
	cmd.Env = gitSafeEnv(opts.CWD)
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
	args := make([]string, 0, len(a.extraArgs)+8)
	args = append(args, "run")
	args = append(args, a.extraArgs...)
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
