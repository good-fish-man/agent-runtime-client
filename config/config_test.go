package config

import (
	"testing"

	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

func TestBootstrapAdministratorReadsEnvironmentOnDemand(t *testing.T) {
	t.Setenv(consts.EnvBootstrapAdminUsername, "local-admin")
	t.Setenv(consts.EnvBootstrapAdminPassword, "generated-test-password")

	username, password := BootstrapAdministrator()
	if username != "local-admin" || password != "generated-test-password" {
		t.Fatalf("unexpected bootstrap values: username=%q password_length=%d", username, len(password))
	}
}

func TestBootstrapAdministratorHasNoDefaultPassword(t *testing.T) {
	t.Setenv(consts.EnvBootstrapAdminUsername, "")
	t.Setenv(consts.EnvBootstrapAdminPassword, "")
	username, password := BootstrapAdministrator()
	if username != "athena" || password != "" {
		t.Fatalf("unexpected bootstrap defaults: username=%q password_length=%d", username, len(password))
	}
}

func TestEvolutionDefaultsAndEnvironmentOverrides(t *testing.T) {
	defaults := Default().Evolution
	if !defaults.Enabled || defaults.ScanIntervalSec != consts.DefaultEvolutionScanIntervalSec ||
		defaults.OwnerBatchSize != consts.DefaultEvolutionOwnerBatchSize ||
		defaults.ExperienceLimit != consts.DefaultEvolutionExperienceLimit ||
		defaults.MaxCandidatesPerScan != consts.DefaultEvolutionCandidatesPerScan ||
		defaults.MinimumNovelExperiences != consts.DefaultEvolutionMinimumNovel {
		t.Fatalf("unexpected evolution defaults: %+v", defaults)
	}

	t.Setenv(consts.EnvEvolutionEnabled, "false")
	t.Setenv(consts.EnvEvolutionScanIntervalSec, "17")
	t.Setenv(consts.EnvEvolutionOwnerBatchSize, "19")
	t.Setenv(consts.EnvEvolutionExperienceLimit, "211")
	t.Setenv(consts.EnvEvolutionCandidatesPerScan, "7")
	t.Setenv(consts.EnvEvolutionMinimumNovel, "3")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Evolution
	if got.Enabled || got.ScanIntervalSec != 17 || got.OwnerBatchSize != 19 || got.ExperienceLimit != 211 || got.MaxCandidatesPerScan != 7 || got.MinimumNovelExperiences != 3 {
		t.Fatalf("unexpected evolution overrides: %+v", got)
	}
}

func TestEvolutionInvalidLimitsFallBackToDefaults(t *testing.T) {
	t.Setenv(consts.EnvEvolutionScanIntervalSec, "0")
	t.Setenv(consts.EnvEvolutionOwnerBatchSize, "201")
	t.Setenv(consts.EnvEvolutionExperienceLimit, "2001")
	t.Setenv(consts.EnvEvolutionCandidatesPerScan, "101")
	t.Setenv(consts.EnvEvolutionMinimumNovel, "0")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Evolution
	if got.ScanIntervalSec != consts.DefaultEvolutionScanIntervalSec || got.OwnerBatchSize != consts.DefaultEvolutionOwnerBatchSize ||
		got.ExperienceLimit != consts.DefaultEvolutionExperienceLimit || got.MaxCandidatesPerScan != consts.DefaultEvolutionCandidatesPerScan ||
		got.MinimumNovelExperiences != consts.DefaultEvolutionMinimumNovel {
		t.Fatalf("invalid evolution values were not normalized: %+v", got)
	}
}

func TestEvolutionCodexIsOptInAndSupportsEnvironmentOverrides(t *testing.T) {
	defaults := Default().Evolution.Codex
	if defaults.Enabled || defaults.Model != consts.DefaultEvolutionCodexModel ||
		defaults.APIBase != consts.DefaultEvolutionCodexAPIBase ||
		defaults.TimeoutSec != consts.DefaultEvolutionCodexTimeoutSec ||
		defaults.MaxOutputTokens != consts.DefaultEvolutionCodexMaxOutputTokens {
		t.Fatalf("unexpected Codex evolution defaults: %+v", defaults)
	}

	t.Setenv(consts.EnvEvolutionCodexEnabled, "true")
	t.Setenv(consts.EnvEvolutionCodexModel, "gpt-5.3-codex")
	t.Setenv(consts.EnvEvolutionCodexAPIKey, "test-key")
	t.Setenv(consts.EnvEvolutionCodexAPIBase, "https://openai.example.test/v1")
	t.Setenv(consts.EnvEvolutionCodexReasoning, "high")
	t.Setenv(consts.EnvEvolutionCodexTimeoutSec, "45")
	t.Setenv(consts.EnvEvolutionCodexMaxOutputTokens, "2048")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Evolution.Codex
	if !got.Enabled || got.Model != "gpt-5.3-codex" || got.APIKey != "test-key" ||
		got.APIBase != "https://openai.example.test/v1" || got.ReasoningEffort != "high" ||
		got.TimeoutSec != 45 || got.MaxOutputTokens != 2048 {
		t.Fatalf("unexpected Codex evolution overrides: %+v", got)
	}
}

func TestEvolutionCodexInvalidLimitsFallBackToDefaults(t *testing.T) {
	t.Setenv(consts.EnvEvolutionCodexTimeoutSec, "0")
	t.Setenv(consts.EnvEvolutionCodexMaxOutputTokens, "10")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Evolution.Codex.TimeoutSec != consts.DefaultEvolutionCodexTimeoutSec ||
		cfg.Evolution.Codex.MaxOutputTokens != consts.DefaultEvolutionCodexMaxOutputTokens {
		t.Fatalf("unexpected normalized Codex evolution config: %+v", cfg.Evolution.Codex)
	}
}
