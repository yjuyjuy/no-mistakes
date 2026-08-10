package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// The per-ticket-size validation profiles under contrib/size-profiles must each
// be a valid global config that LoadGlobal accepts, and each must actually tune
// what its size promises: a default model plus, for medium and large, a
// promoted review step via agent_args_override_per_step. This guards against a
// template drifting out of sync with the config schema (KnownFields + reserved
// flag validation) or silently losing its per-step review promotion.
func TestSizeProfiles_LoadAndTuneAsDocumented(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		size            string
		wantPerStepKeys []types.StepName
	}{
		{"small", nil},
		{"medium", []types.StepName{types.StepReview}},
		{"large", []types.StepName{types.StepReview, types.StepTest, types.StepRebase}},
	}

	for _, p := range profiles {
		p := p
		t.Run(p.size, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("contrib", "size-profiles", p.size+".config.yaml")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("profile template missing: %v", err)
			}
			cfg, err := config.LoadGlobal(path)
			if err != nil {
				t.Fatalf("LoadGlobal(%s): %v", path, err)
			}
			merged := config.Merge(cfg, &config.RepoConfig{})

			// Every profile sets a default model for the resolved agent.
			claudeArgs := merged.AgentArgsFor(types.AgentClaude)
			codexArgs := merged.AgentArgsFor(types.AgentCodex)
			if len(claudeArgs) == 0 && len(codexArgs) == 0 {
				t.Fatalf("%s profile sets no default agent_args_override", p.size)
			}

			// The promised per-step promotions must be present and non-empty.
			for _, step := range p.wantPerStepKeys {
				got := merged.AgentArgsForStep(step)
				if len(got) == 0 {
					t.Errorf("%s profile: expected agent_args_override_per_step for step %q", p.size, step)
				}
			}
			// Small stays flat: no per-step promotion.
			if p.size == "small" {
				if got := merged.AgentArgsForStep(types.StepReview); got != nil {
					t.Errorf("small profile should not promote any step, got review=%v", got)
				}
			}
		})
	}
}
