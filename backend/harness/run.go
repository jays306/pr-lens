package harness

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/pr-lens/backend/analysis"
	"github.com/pr-lens/backend/gitclone"
)

// Provider runs a local Claude Code or Codex process as the reviewer.
type Provider struct {
	cfg Config
}

// New returns a harness reviewer. The binary is resolved at run time from PATH.
func New(cfg Config) *Provider {
	return &Provider{cfg: cfg}
}

func (p *Provider) Name() string {
	if p.cfg.Model != "" {
		return "harness(" + p.cfg.Kind + "/" + p.cfg.Model + ")"
	}
	return "harness(" + p.cfg.Kind + ")"
}

// AnalyzePR is unused; reviews need a clone directory.
func (p *Provider) AnalyzePR(context.Context, string, func(analysis.StreamEvent) error) error {
	return fmt.Errorf("harness review needs a cloned PR head")
}

// AnalyzePRInDir spawns the harness in dir and emits PR-LENS JSON events.
func (p *Provider) AnalyzePRInDir(ctx context.Context, dir, userPrompt string, emit func(analysis.StreamEvent) error) error {
	if dir == "" {
		return fmt.Errorf("harness review needs a cloned PR head")
	}
	token, owner := authFrom(ctx)
	extra, cleanup, err := gitclone.AuthEnv(token, owner)
	if err != nil {
		return fmt.Errorf("harness auth env: %w", err)
	}
	defer cleanup()

	args := Argv(p.cfg)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = mergeEnv(os.Environ(), extra)
	cmd.Env = mergeEnv(cmd.Env, ExtraEnv(p.cfg))
	cmd.Stdin = strings.NewReader(analysis.SystemPrompt() + "\n\n---\n\n" + userPrompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf strings.Builder
	cmd.Stderr = &errBuf

	log.Printf("[harness] start %s model=%s bedrock=%v cwd=%s", p.cfg.Kind, p.cfg.Model, p.cfg.Bedrock, dir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", p.cfg.Bin, err)
	}

	var parseErr error
	if p.cfg.Kind == KindCodex {
		parseErr = consumeCodex(stdout, emit)
	} else {
		parseErr = consumeClaude(stdout, emit)
	}
	waitErr := cmd.Wait()
	if parseErr != nil {
		return parseErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(redact(errBuf.String(), token))
		if msg == "" {
			msg = waitErr.Error()
		}
		return fmt.Errorf("%s exited: %s", p.cfg.Kind, msg)
	}
	return nil
}

func consumeClaude(r io.Reader, emit func(analysis.StreamEvent) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var buf strings.Builder
	sawPartial := false
	var assistant string
	var fail string
	for sc.Scan() {
		line := sc.Text()
		if d := claudeTextDelta(line); d != "" {
			sawPartial = true
			if err := emitJSONEvents(d, &buf, emit); err != nil {
				return err
			}
			continue
		}
		if msg := claudeAssistantError(line); msg != "" {
			fail = msg
			continue
		}
		if t := claudeAssistantText(line); t != "" {
			assistant = t
		}
		if text, isErr, ok := claudeResult(line); ok {
			if isErr && text != "" {
				fail = text
				continue
			}
			if !sawPartial && text != "" {
				if err := emitJSONEvents(text, &buf, emit); err != nil {
					return err
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if fail != "" {
		return fmt.Errorf("%s", fail)
	}
	if !sawPartial && assistant != "" {
		if err := emitJSONEvents(assistant, &buf, emit); err != nil {
			return err
		}
	}
	return emitJSONEvents("", &buf, emit)
}

func consumeCodex(r io.Reader, emit func(analysis.StreamEvent) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var last string
	for sc.Scan() {
		if t := codexAgentMessage(sc.Text()); t != "" {
			last = t
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	var buf strings.Builder
	if last != "" {
		if err := emitJSONEvents(last, &buf, emit); err != nil {
			return err
		}
	}
	return emitJSONEvents("", &buf, emit)
}

func redact(msg, token string) string {
	if token == "" {
		return msg
	}
	return strings.ReplaceAll(msg, token, "<token>")
}
