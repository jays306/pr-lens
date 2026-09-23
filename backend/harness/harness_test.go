package harness

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pr-lens/backend/analysis"
)

func TestKindFromProvider(t *testing.T) {
	if kindFromProvider("openai") != KindCodex {
		t.Fatal("openai should select codex")
	}
	if kindFromProvider("claude") != KindClaude || kindFromProvider("") != KindClaude {
		t.Fatal("claude / empty should select claude")
	}
}

func TestFromEnv_AgentHarnessWins(t *testing.T) {
	t.Setenv("AI_PROVIDER", "openai")
	t.Setenv("AGENT_HARNESS", "claude")
	t.Setenv("CLAUDE_BIN", "/opt/claude")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != KindClaude || cfg.Bin != "/opt/claude" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestFromEnv_Unknown(t *testing.T) {
	t.Setenv("AGENT_HARNESS", "cursor")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveClaudeModel_Bedrock(t *testing.T) {
	got := resolveClaudeModel("bedrock", "anthropic.claude-opus-5")
	if got != "us.anthropic.claude-opus-5" {
		t.Fatalf("got %q", got)
	}
	if resolveClaudeModel("bedrock", "us.anthropic.claude-opus-5") != "us.anthropic.claude-opus-5" {
		t.Fatal("already-prefixed model should stay")
	}
	if resolveClaudeModel("claude", "claude-sonnet-4-6") != "claude-sonnet-4-6" {
		t.Fatal("anthropic API model should stay")
	}
	if resolveClaudeModel("claude", "bedrock/us.anthropic.claude-opus-5") != "bedrock/us.anthropic.claude-opus-5" {
		t.Fatal("explicit provider path should stay")
	}
}

func TestArgv_ClaudeModel(t *testing.T) {
	args := Argv(Config{Kind: KindClaude, Bin: "claude", Model: "us.anthropic.claude-opus-5", Bedrock: true})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model us.anthropic.claude-opus-5") {
		t.Fatalf("missing model: %s", joined)
	}
	if !strings.Contains(joined, "--bare") {
		t.Fatalf("bedrock must skip user settings: %s", joined)
	}
	if !emptySettingSources(args) {
		t.Fatalf("bedrock must pass empty --setting-sources: %s", joined)
	}
}

func emptySettingSources(args []string) bool {
	for i, a := range args {
		if a == "--setting-sources" && i+1 < len(args) && args[i+1] == "" {
			return true
		}
	}
	return false
}

func TestMergeEnv_Replaces(t *testing.T) {
	got := mergeEnv(
		[]string{"ANTHROPIC_BEDROCK_SERVICE_TIER=priority", "FOO=1"},
		[]string{"ANTHROPIC_BEDROCK_SERVICE_TIER=default"},
	)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "ANTHROPIC_BEDROCK_SERVICE_TIER=default") {
		t.Fatalf("got %s", joined)
	}
	if strings.Contains(joined, "priority") {
		t.Fatalf("priority left in env: %s", joined)
	}
}

func TestExtraEnv_Bedrock(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("BEDROCK_API_KEY", "test-bedrock-key")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("BEDROCK_REGION", "us-east-1")
	t.Setenv("ANTHROPIC_BEDROCK_SERVICE_TIER", "")
	env := ExtraEnv(Config{Bedrock: true, Model: "us.anthropic.claude-opus-5"})
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"CLAUDE_CODE_USE_BEDROCK=1",
		"ANTHROPIC_MODEL=us.anthropic.claude-opus-5",
		"AWS_BEARER_TOKEN_BEDROCK=test-bedrock-key",
		"AWS_REGION=us-east-1",
		"ANTHROPIC_BEDROCK_SERVICE_TIER=default",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
}

func TestArgv_Claude(t *testing.T) {
	args := Argv(Config{Kind: KindClaude, Bin: "claude"})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"claude -p",
		"--output-format stream-json",
		"--tools Read,Grep,Glob,Bash",
		"--dangerously-skip-permissions",
		"--permission-mode bypassPermissions",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "ghp_") || strings.Contains(strings.ToLower(joined), "askpass_password") {
		t.Fatalf("argv leaked a secret: %s", joined)
	}
}

func TestArgv_Codex(t *testing.T) {
	args := Argv(Config{Kind: KindCodex, Bin: "codex"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "codex exec") || !strings.Contains(joined, "--json") {
		t.Fatalf("codex argv: %s", joined)
	}
	if !strings.Contains(joined, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("codex must allow network installs in the temp clone: %s", joined)
	}
	if strings.Contains(joined, "ghp_") {
		t.Fatalf("argv leaked a token: %s", joined)
	}
}

func TestConsumeClaude_StreamJSON(t *testing.T) {
	risk := `{"type":"risk","data":{"score":10,"level":"low","label":"ok"}}`
	done := `{"type":"done","data":{}}`
	in := streamDelta(risk+"\n") + "\n" + streamDelta(done+"\n") + "\n"
	var got []analysis.StreamEvent
	if err := consumeClaude(strings.NewReader(in), func(ev analysis.StreamEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Type != "risk" || got[1].Type != "done" {
		t.Fatalf("got %#v", got)
	}
}

func TestConsumeCodex_LastAgentMessage(t *testing.T) {
	first := `{"type":"item.completed","item":{"type":"agent_message","text":"{\"type\":\"summary\",\"data\":{\"text\":\"old\"}}\n"}}`
	last := `{"type":"item.completed","item":{"type":"agent_message","text":"{\"type\":\"risk\",\"data\":{\"score\":3,\"level\":\"low\",\"label\":\"n\"}}\n{\"type\":\"done\",\"data\":{}}\n"}}`
	var got []analysis.StreamEvent
	if err := consumeCodex(strings.NewReader(first+"\n"+last+"\n"), func(ev analysis.StreamEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Type != "risk" || got[1].Type != "done" {
		t.Fatalf("got %#v", got)
	}
}

func TestConsumeClaude_SurfacesStdoutError(t *testing.T) {
	in := `{"type":"assistant","error":"authentication_failed","message":{"content":[{"type":"text","text":"Failed to authenticate. API Error: 403 from proxy"}]}}
{"type":"result","subtype":"success","is_error":true,"result":"Failed to authenticate. API Error: 403 from proxy"}
`
	err := consumeClaude(strings.NewReader(in), func(analysis.StreamEvent) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "Failed to authenticate") {
		t.Fatalf("got %v", err)
	}
}

func streamDelta(text string) string {
	raw, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":` + string(raw) + `}}}`
}
