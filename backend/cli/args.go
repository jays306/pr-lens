package cli

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrHelp means the user asked for usage text.
var ErrHelp = errors.New("help")

// Args is a parsed `pr-lens` invocation.
type Args struct {
	URL   string
	Token string
	JSON  bool
	Post  bool
}

var (
	hashPR = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)#(\d+)$`)
	pathPR = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/(\d+)$`)
)

// ParseArgs reads a PR URL and flags from argv (without the program name).
func ParseArgs(argv []string) (Args, error) {
	var a Args
	var positional []string
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "-h" || arg == "--help":
			return Args{}, ErrHelp
		case arg == "--json":
			a.JSON = true
		case arg == "--post":
			a.Post = true
		case arg == "--token":
			if i+1 >= len(argv) {
				return Args{}, fmt.Errorf("--token requires a value")
			}
			i++
			a.Token = argv[i]
		case strings.HasPrefix(arg, "--token="):
			a.Token = strings.TrimPrefix(arg, "--token=")
		case strings.HasPrefix(arg, "-"):
			return Args{}, fmt.Errorf("unknown flag %s", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) > 0 && positional[0] == "review" {
		positional = positional[1:]
	}
	if len(positional) != 1 {
		return Args{}, fmt.Errorf("usage: pr-lens [review] <pr-url> [--json] [--post] [--token TOKEN]")
	}
	a.URL = NormalizePRURL(positional[0])
	return a, nil
}

// NormalizePRURL expands owner/repo#123 and owner/repo/123 into a GitHub URL.
func NormalizePRURL(raw string) string {
	s := strings.TrimSpace(raw)
	if m := hashPR.FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("https://github.com/%s/%s/pull/%s", m[1], m[2], m[3])
	}
	if m := pathPR.FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("https://github.com/%s/%s/pull/%s", m[1], m[2], m[3])
	}
	return s
}

// Usage is printed for --help or a bad invocation.
func Usage() string {
	return `pr-lens — review a GitHub pull request from the terminal

Usage:
  pr-lens [review] <pr-url> [--json] [--post] [--token TOKEN]

<pr-url> may be a full GitHub URL, owner/repo#123, or owner/repo/123.

Flags:
  --json            print the analysis events as JSON
  --post            post the findings and recommendation to GitHub
  --token TOKEN     GitHub PAT (default: GITHUB_TOKEN)

Uses the same backend/.env as the HTTP server (AGENT_HARNESS, GITHUB_TOKEN).
`
}
