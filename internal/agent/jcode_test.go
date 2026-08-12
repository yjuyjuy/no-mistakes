package agent

import (
	"context"
	"strings"
	"testing"
)

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
