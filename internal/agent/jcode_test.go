package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestJcodeEffortLevel_ParsesPinnedLevel(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no effort pinned", []string{"-m", "claude-sonnet-5"}, ""},
		{"separate value", []string{"--effort", "high"}, "high"},
		{"equals form", []string{"--effort=medium"}, "medium"},
		{"mixed with model", []string{"-m", "claude-opus-4-8", "--effort", "low"}, "low"},
		{"last wins separate", []string{"--effort", "low", "--effort", "high"}, "high"},
		{"last wins equals", []string{"--effort=low", "--effort=xhigh"}, "xhigh"},
		{"dangling flag keeps prior pin", []string{"--effort", "low", "--effort"}, "low"},
		{"operator value passes through untranslated", []string{"--effort", "Default"}, "Default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jcodeEffortLevel(tt.args); got != tt.want {
				t.Errorf("jcodeEffortLevel(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestJcodeEffectiveEffort_DefaultsLow(t *testing.T) {
	if got := jcodeEffectiveEffort(nil); got != "low" {
		t.Errorf("jcodeEffectiveEffort(nil) = %q, want the old claude-era default low", got)
	}
	if got := jcodeEffectiveEffort([]string{"-m", "claude-sonnet-5"}); got != "low" {
		t.Errorf("jcodeEffectiveEffort with model only = %q, want low", got)
	}
}

func TestJcodeEffectiveEffort_HonorsPinnedLevel(t *testing.T) {
	for _, args := range [][]string{
		{"--effort", "high"},
		{"--effort=medium"},
		{"-m", "claude-opus-4-8", "--effort", "xhigh"},
		{"--effort", "default"}, // "default" restores jcode's model default
	} {
		if got := jcodeEffectiveEffort(args); got == "" || got == "low" {
			t.Errorf("jcodeEffectiveEffort(%v) = %q, want the pinned level", args, got)
		}
	}
}

func TestJcodeEffortEnv_NamesBothReasoningEffortProviderFamilies(t *testing.T) {
	env := jcodeEffortEnv("low")
	if len(env) != 2 {
		t.Fatalf("jcodeEffortEnv(len) = %v, want exactly the anthropic and openai entries", env)
	}
	want := map[string]string{
		"JCODE_ANTHROPIC_REASONING_EFFORT": "low",
		"JCODE_OPENAI_REASONING_EFFORT":    "low",
	}
	got := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("env entry %q has no =", kv)
		}
		got[k] = v
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("env[%s] = %q, want %q (full env: %v)", k, got[k], v, env)
		}
	}
}

func TestJcodeAgent_BuildArgs_Cold(t *testing.T) {
	ja := &jcodeAgent{bin: "jcode"}
	args := ja.buildArgs("do the thing", "")

	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "run ") {
		t.Fatalf("cold args must start with the run subcommand: %v", args)
	}
	if !strings.Contains(joined, "--ndjson") {
		t.Fatalf("args must request the ndjson event stream: %v", args)
	}
	if !strings.Contains(joined, "--quiet") {
		t.Fatalf("args must pass --quiet to suppress status chrome: %v", args)
	}
	if !strings.Contains(joined, "--no-update") {
		t.Fatalf("args must pass --no-update so the run never blocks on an update: %v", args)
	}
	for _, a := range args {
		if a == "--resume" {
			t.Fatalf("cold invocation must not pass --resume: %v", args)
		}
	}
	// The prompt must be delivered as the positional after a `--` separator so a
	// prompt that begins with a dash can never be parsed as a flag.
	if args[len(args)-2] != "--" || args[len(args)-1] != "do the thing" {
		t.Fatalf("prompt must be the final positional after --: %v", args)
	}
}

func TestJcodeAgent_BuildArgs_Resume(t *testing.T) {
	ja := &jcodeAgent{bin: "jcode"}
	args := ja.buildArgs("re-review the branch", "session_eagle_123")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--resume session_eagle_123") {
		t.Fatalf("resume args missing --resume <id>: %v", args)
	}
	if !strings.Contains(joined, "--ndjson") {
		t.Fatalf("resume args must keep the ndjson stream: %v", args)
	}
	if strings.Contains(joined, "re-review the branch --resume") {
		t.Fatalf("prompt must not appear before the flags: %v", args)
	}
	if args[len(args)-1] != "re-review the branch" {
		t.Fatalf("resume prompt must stay the final positional: %v", args)
	}
}

func TestJcodeAgent_BuildArgs_KeepsExtraArgs(t *testing.T) {
	ja := &jcodeAgent{bin: "jcode", extraArgs: []string{"-m", "claude-opus-4-8"}}
	args := ja.buildArgs("prompt", "")

	joined := strings.Join(args, " ")
	// Extra args (e.g. -m/--model) must sit between the run subcommand and the
	// managed flags so an operator model choice takes effect.
	if !strings.HasPrefix(joined, "run -m claude-opus-4-8 ") {
		t.Fatalf("extra args must follow the run subcommand: %v", args)
	}
}

func TestJcodeAgent_SupportsSessionResume(t *testing.T) {
	ja := &jcodeAgent{bin: "jcode"}
	if !ja.SupportsSessionResume() {
		t.Fatal("jcode advertises native session resume (jcode run --resume <id>)")
	}
}

func TestJcodeAgent_WithStepArgs(t *testing.T) {
	ja := &jcodeAgent{bin: "jcode", extraArgs: []string{"-m", "claude-sonnet-4-6"}}
	next := ja.withStepArgs(RunOpts{StepArgsOverride: map[string][]string{
		"jcode": {"-m", "claude-opus-4-8"},
	}})
	if next == ja {
		t.Fatal("a differing per-step profile must yield a new adapter value")
	}
	args := next.buildArgs("p", "")
	if !strings.Contains(strings.Join(args, " "), "-m claude-opus-4-8") {
		t.Fatalf("per-step override must replace the constructed args: %v", args)
	}
	// The original adapter must be unchanged.
	if strings.Contains(strings.Join(ja.buildArgs("p", ""), " "), "opus") {
		t.Fatal("withStepArgs must not mutate the receiver")
	}
}

func TestParseJcodeEvents_CapturesTextSessionAndUsage(t *testing.T) {
	events := `{"model":"claude-opus-4-8","provider":"Claude","session_id":"session_eagle_1","type":"start"}
{"phase":"streaming","type":"connection_phase"}
{"text":"hel","type":"text_delta"}
{"text":"lo","type":"text_delta"}
{"stop_reason":"end_turn","type":"message_end"}
{"cache_creation_input":10,"cache_read_input":5,"input":100,"output":20,"type":"tokens"}
{"connection_phase":"streaming","model":"claude-opus-4-8","provider":"Claude","session_id":"session_eagle_1","text":"hello","type":"done","usage":{"cache_creation_input_tokens":10,"cache_read_input_tokens":5,"input_tokens":100,"output_tokens":20}}
`
	var chunks []string
	res := &jcodeResult{}
	if err := parseJcodeEvents(context.Background(), strings.NewReader(events), func(s string) { chunks = append(chunks, s) }, res); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.text != "hello" {
		t.Fatalf("text = %q, want hello", res.text)
	}
	if res.sessionID != "session_eagle_1" {
		t.Fatalf("sessionID = %q, want session_eagle_1", res.sessionID)
	}
	if res.model != "claude-opus-4-8" {
		t.Fatalf("model = %q, want claude-opus-4-8", res.model)
	}
	if res.provider != "Claude" {
		t.Fatalf("provider = %q, want Claude", res.provider)
	}
	if res.usage.InputTokens != 100 || res.usage.OutputTokens != 20 {
		t.Fatalf("usage input/output = %d/%d, want 100/20", res.usage.InputTokens, res.usage.OutputTokens)
	}
	if res.usage.CacheReadTokens != 5 || res.usage.CacheCreationTokens != 10 {
		t.Fatalf("usage cache read/creation = %d/%d, want 5/10", res.usage.CacheReadTokens, res.usage.CacheCreationTokens)
	}
	if !res.usage.Reported || !res.usage.CacheCreationReported {
		t.Fatalf("usage must be marked reported incl. cache creation: %+v", res.usage)
	}
	if strings.Join(chunks, "") != "hello" {
		t.Fatalf("streamed chunks = %q, want hello", strings.Join(chunks, ""))
	}
}

func TestParseJcodeEvents_SumsPerTurnTokensAcrossToolRounds(t *testing.T) {
	// A run that calls a tool emits two `tokens` events (one per model round).
	// The adapter must accumulate them, not take only the final turn.
	events := `{"session_id":"s1","model":"m","type":"start"}
{"text":"reading","type":"text_delta"}
{"cache_creation_input":100,"cache_read_input":0,"input":500,"output":70,"type":"tokens"}
{"id":"t1","name":"read","type":"tool_done","output":"...","error":null}
{"text":"hello","type":"text_delta"}
{"cache_creation_input":5,"cache_read_input":400,"input":60,"output":4,"type":"tokens"}
{"session_id":"s1","model":"m","text":"hello","type":"done"}
`
	res := &jcodeResult{}
	if err := parseJcodeEvents(context.Background(), strings.NewReader(events), nil, res); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.usage.InputTokens != 560 || res.usage.OutputTokens != 74 {
		t.Fatalf("summed input/output = %d/%d, want 560/74", res.usage.InputTokens, res.usage.OutputTokens)
	}
	// The final done text wins over intermediate deltas.
	if res.text != "hello" {
		t.Fatalf("text = %q, want hello", res.text)
	}
}

func TestParseJcodeEvents_CapturesError(t *testing.T) {
	events := `{"session_id":"s1","model":"m","type":"start"}
{"message":"boom failed to send request","model":"m","session_id":"s1","type":"error"}
`
	res := &jcodeResult{}
	if err := parseJcodeEvents(context.Background(), strings.NewReader(events), nil, res); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(res.errMessage, "boom failed to send request") {
		t.Fatalf("errMessage = %q, want the error event message", res.errMessage)
	}
}

func TestJcodeAgent_BuildArgs_StripsEffortPseudoFlag(t *testing.T) {
	// jcode run has no --effort flag, so the pseudo-flag must never reach argv:
	// it is translated to the JCODE_*_REASONING_EFFORT env vars instead (see
	// TestJcodeAgent_RunOnce_ThreadsEffortEnv).
	for _, extra := range [][]string{
		{"-m", "claude-sonnet-5", "--effort", "high"},
		{"--effort=high", "-m", "claude-sonnet-5"},
		{"--effort", "high"},
	} {
		ja := &jcodeAgent{bin: "jcode", extraArgs: extra}
		args := ja.buildArgs("do the thing", "")
		joined := strings.Join(args, " ")
		if !strings.HasPrefix(joined, "run ") {
			t.Fatalf("args must start with the run subcommand: %v", args)
		}
		for _, arg := range args {
			if strings.Contains(arg, "effort") {
				t.Errorf("extraArgs %v: argv %v must not carry the effort pseudo-flag", extra, args)
			}
		}
		if args[len(args)-2] != "--" || args[len(args)-1] != "do the thing" {
			t.Fatalf("extraArgs %v: prompt must stay the final positional after --: %v", extra, args)
		}
	}
}

func TestJcodeAgent_BuildArgs_KeepsNonEffortExtraArgs(t *testing.T) {
	ja := &jcodeAgent{bin: "jcode", extraArgs: []string{"-m", "claude-opus-4-8", "--effort", "high"}}
	args := ja.buildArgs("p", "")
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "run -m claude-opus-4-8 ") {
		t.Fatalf("non-effort extra args must survive and follow the run subcommand: %v", args)
	}
}

type jcodeEnvObservation struct {
	AnthropicEffort string   `json:"anthropic_effort"`
	OpenAIEffort    string   `json:"openai_effort"`
	Marker          string   `json:"marker"`
	Args            []string `json:"args"`
}

func TestJcodeAgent_RunOnce_ThreadsEffortEnv(t *testing.T) {
	// The probe replays the test binary as the jcode CLI and records the
	// environment it actually saw plus the argv it was launched with, so these
	// cases cover the full invocation construction: managed flags, effort env
	// translation, opts.Env passthrough, and the per-step arg profile.
	tests := []struct {
		name       string
		extraArgs  []string
		stepArgs   map[string][]string
		wantEffort string
	}{
		{
			name:       "default low matches the old claude override",
			extraArgs:  []string{"-m", "claude-sonnet-5"},
			wantEffort: "low",
		},
		{
			name:       "pinned effort overrides the default",
			extraArgs:  []string{"-m", "claude-sonnet-5", "--effort", "high"},
			wantEffort: "high",
		},
		{
			name:       "equals form",
			extraArgs:  []string{"--effort=medium"},
			wantEffort: "medium",
		},
		{
			name:       "per-step profile pins effort",
			extraArgs:  []string{"-m", "claude-sonnet-5", "--effort", "low"},
			stepArgs:   map[string][]string{"jcode": {"-m", "claude-opus-4-8", "--effort", "xhigh"}},
			wantEffort: "xhigh",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obsPath := filepath.Join(t.TempDir(), "observation.json")
			opts := RunOpts{
				Prompt: "probe the effort axis",
				CWD:    t.TempDir(),
				Env: []string{
					"NM_JCODE_ENV_OBSERVATION=" + obsPath,
					"NM_JCODE_PROBE_MARKER=present",
				},
				StepArgsOverride: tt.stepArgs,
			}
			a := newJcodeEnvProbeAgent(t, tt.extraArgs)
			result, err := a.runOnce(context.Background(), opts)
			if err != nil {
				t.Fatalf("runOnce: %v", err)
			}
			if result.Text != "ok" || result.SessionID != "probe-session" {
				t.Fatalf("result = %+v, want the probe's done event", result)
			}

			data, err := os.ReadFile(obsPath)
			if err != nil {
				t.Fatalf("read probe observation: %v", err)
			}
			var got jcodeEnvObservation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("parse probe observation: %v", err)
			}
			if got.AnthropicEffort != tt.wantEffort {
				t.Errorf("JCODE_ANTHROPIC_REASONING_EFFORT = %q, want %q", got.AnthropicEffort, tt.wantEffort)
			}
			if got.OpenAIEffort != tt.wantEffort {
				t.Errorf("JCODE_OPENAI_REASONING_EFFORT = %q, want %q", got.OpenAIEffort, tt.wantEffort)
			}
			if got.Marker != "present" {
				t.Errorf("opts.Env passthrough marker = %q, want present (jcode must thread invocation env)", got.Marker)
			}
			// Only the flag region (everything before the trailing `-- <prompt>`
			// positional) may not carry the effort pseudo-flag; the prompt itself
			// is free-form.
			if len(got.Args) >= 2 {
				for _, arg := range got.Args[:len(got.Args)-2] {
					if arg == "--effort" || strings.HasPrefix(arg, "--effort=") {
						t.Errorf("spawned argv %v must not carry the effort pseudo-flag", got.Args)
					}
				}
			}
			joined := strings.Join(got.Args, " ")
			for _, want := range []string{"run", "--ndjson", "--quiet", "--no-update", "--no-selfdev"} {
				if !strings.Contains(joined, want) {
					t.Errorf("spawned argv %v missing managed flag %q", got.Args, want)
				}
			}
			if got.Args[len(got.Args)-2] != "--" || got.Args[len(got.Args)-1] != "probe the effort axis" {
				t.Errorf("spawned argv %v must end with -- <prompt>", got.Args)
			}
			if len(got.Args) > 1 && got.Args[0] != "run" {
				t.Errorf("spawned argv %v must start with the run subcommand", got.Args)
			}
		})
	}
}

// newJcodeEnvProbeAgent returns a jcodeAgent whose binary is a wrapper script
// that replays the current test executable in helper mode, so runOnce's argv
// and environment can be observed without a real jcode install.
//
// Windows cannot launch an extensionless #!/bin/sh script: exec.LookPath needs
// a PATHEXT extension (the bare name fails with "executable file not found in
// %PATH%"), and even a renamed .exe would not run a shell-script body. So on
// Windows the wrapper is a .cmd batch, mirroring the sibling probe fakes in
// codex_test.go, pi_test.go, and copilot_test.go, which all launch through the
// same native-agent exec path. Both wrappers forward every argument the adapter
// passed, after their own `-test.run` selector and a `--` separator, so the
// helper sees the full jcode argv via argsAfterDoubleDash.
func newJcodeEnvProbeAgent(t *testing.T, extraArgs []string) *jcodeAgent {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("current test executable: %v", err)
	}
	dir := t.TempDir()
	var script, body string
	if runtime.GOOS == "windows" {
		// %* forwards the adapter's argv verbatim. The -test.run selector is
		// double-quoted so cmd.exe treats the regex anchors ^ and $ literally
		// rather than as its escape/redirection metacharacters.
		script = filepath.Join(dir, "jcode-probe.cmd")
		body = "@echo off\r\n\"" + exe + "\" \"-test.run=^TestJcodeRunOnceEnvProbe$\" -- %*\r\n"
	} else {
		script = filepath.Join(dir, "jcode-probe")
		body = "#!/bin/sh\nexec " + strconv.Quote(exe) + " -test.run=^TestJcodeRunOnceEnvProbe$ -- \"$@\"\n"
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write probe script: %v", err)
	}
	return &jcodeAgent{bin: script, extraArgs: extraArgs}
}

// TestJcodeRunOnceEnvProbe is the helper mode of TestJcodeAgent_RunOnce_ThreadsEffortEnv:
// it records the effort env vars, an invocation-env marker, and the adapter's
// argv, then emits the one NDJSON done event runOnce needs to succeed.
func TestJcodeRunOnceEnvProbe(t *testing.T) {
	path := os.Getenv("NM_JCODE_ENV_OBSERVATION")
	if path == "" {
		return
	}
	obs := jcodeEnvObservation{
		AnthropicEffort: os.Getenv("JCODE_ANTHROPIC_REASONING_EFFORT"),
		OpenAIEffort:    os.Getenv("JCODE_OPENAI_REASONING_EFFORT"),
		Marker:          os.Getenv("NM_JCODE_PROBE_MARKER"),
		Args:            argsAfterDoubleDash(os.Args),
	}
	data, _ := json.Marshal(obs)
	_ = os.WriteFile(path, data, 0o644)
	fmt.Println(`{"session_id":"probe-session","model":"probe-model","provider":"Claude","text":"ok","type":"done"}`)
	os.Exit(0)
}
