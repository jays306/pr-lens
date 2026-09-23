package harness

import (
	"fmt"
	"os"
	"strings"
)

const (
	KindClaude = "claude"
	KindCodex  = "codex"
)

// Config describes which local agent binary to spawn.
type Config struct {
	Kind    string
	Bin     string
	Model   string
	Bedrock bool
}

// FromEnv picks Claude Code or Codex from AGENT_HARNESS, else AI_PROVIDER.
func FromEnv() (Config, error) {
	kind := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_HARNESS")))
	if kind == "" {
		kind = kindFromProvider(os.Getenv("AI_PROVIDER"))
	}
	provider := os.Getenv("AI_PROVIDER")
	switch kind {
	case KindClaude:
		bin := strings.TrimSpace(os.Getenv("CLAUDE_BIN"))
		if bin == "" {
			bin = "claude"
		}
		bedrock := strings.EqualFold(strings.TrimSpace(provider), "bedrock")
		return Config{
			Kind:    KindClaude,
			Bin:     bin,
			Model:   resolveClaudeModel(provider, os.Getenv("ANTHROPIC_MODEL")),
			Bedrock: bedrock,
		}, nil
	case KindCodex:
		bin := strings.TrimSpace(os.Getenv("CODEX_BIN"))
		if bin == "" {
			bin = "codex"
		}
		return Config{Kind: KindCodex, Bin: bin}, nil
	default:
		return Config{}, fmt.Errorf("unknown harness %q (set AGENT_HARNESS=claude or codex)", kind)
	}
}

func kindFromProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return KindCodex
	default:
		return KindClaude
	}
}

// Argv is the subprocess argument list. The review prompt is passed on stdin,
// never as an argument, so a GitHub token cannot leak via ps.
func Argv(cfg Config) []string {
	switch cfg.Kind {
	case KindCodex:
		return []string{
			cfg.Bin, "exec",
			"--json",
			"--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox",
			"-",
		}
	default:
		args := []string{
			cfg.Bin, "-p",
			"--output-format", "stream-json",
			"--input-format", "text",
			"--verbose",
			"--include-partial-messages",
			"--tools", "Read,Grep,Glob,Bash",
			"--disallowedTools", "Edit,Write",
			"--permission-mode", "bypassPermissions",
			"--dangerously-skip-permissions",
		}
		if cfg.Bedrock {
			// --bare does not skip ~/.claude/settings.json. An empty
			// --setting-sources list does, so a user priority Bedrock
			// service tier is not applied (opus-5 rejects it).
			args = append(args, "--bare", "--setting-sources", "")
		} else {
			args = append(args, "--setting-sources", "user")
		}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		return args
	}
}

// resolveClaudeModel turns backend/.env model IDs into what Claude Code / LiteLLM
// will accept. Bedrock IDs get a cross-region prefix. A path that already has a
// provider slash (bedrock/..., anthropic/...) is left alone.
func resolveClaudeModel(provider, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if strings.Contains(model, "/") {
		return model
	}
	if strings.EqualFold(strings.TrimSpace(provider), "bedrock") || strings.HasPrefix(model, "anthropic.") {
		return bedrockModelID(model)
	}
	return model
}

func bedrockModelID(model string) string {
	if strings.HasPrefix(model, "anthropic.") &&
		!strings.HasPrefix(model, "us.") &&
		!strings.HasPrefix(model, "eu.") &&
		!strings.HasPrefix(model, "ap.") {
		return "us." + model
	}
	return model
}

// ExtraEnv is appended to the harness process environment. The Bedrock bearer
// token is copied from BEDROCK_API_KEY when AWS_BEARER_TOKEN_BEDROCK is unset.
func ExtraEnv(cfg Config) []string {
	if !cfg.Bedrock {
		if cfg.Model != "" {
			return []string{"ANTHROPIC_MODEL=" + cfg.Model}
		}
		return nil
	}
	var extra []string
	extra = append(extra, "CLAUDE_CODE_USE_BEDROCK=1")
	if cfg.Model != "" {
		extra = append(extra, "ANTHROPIC_MODEL="+cfg.Model)
	}
	if os.Getenv("AWS_BEARER_TOKEN_BEDROCK") == "" {
		if k := strings.TrimSpace(os.Getenv("BEDROCK_API_KEY")); k != "" {
			extra = append(extra, "AWS_BEARER_TOKEN_BEDROCK="+k)
		}
	}
	if os.Getenv("AWS_REGION") == "" && os.Getenv("AWS_DEFAULT_REGION") == "" {
		region := strings.TrimSpace(os.Getenv("BEDROCK_REGION"))
		if region == "" {
			region = "us-east-1"
		}
		extra = append(extra, "AWS_REGION="+region, "AWS_DEFAULT_REGION="+region)
	}
	// Always default. User Claude settings often set priority; opus-5
	// on Bedrock returns 400 for that tier.
	extra = append(extra, "ANTHROPIC_BEDROCK_SERVICE_TIER=default")
	return extra
}

// mergeEnv overlays extra on base. Later keys replace earlier ones so a
// parent-process ANTHROPIC_BEDROCK_SERVICE_TIER=priority does not win.
func mergeEnv(base, extra []string) []string {
	idx := map[string]int{}
	out := make([]string, 0, len(base)+len(extra))
	put := func(kv string) {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			return
		}
		if i, exists := idx[k]; exists {
			out[i] = kv
			return
		}
		idx[k] = len(out)
		out = append(out, kv)
	}
	for _, kv := range base {
		put(kv)
	}
	for _, kv := range extra {
		put(kv)
	}
	return out
}
