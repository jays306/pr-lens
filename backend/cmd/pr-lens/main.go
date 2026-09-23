package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"

	"github.com/pr-lens/backend/analysis"
	"github.com/pr-lens/backend/analyze"
	"github.com/pr-lens/backend/app"
	"github.com/pr-lens/backend/cli"
	"github.com/pr-lens/backend/github"
)

func main() {
	log.SetPrefix("pr-lens: ")
	log.SetFlags(0)

	args, err := cli.ParseArgs(os.Args[1:])
	if errors.Is(err, cli.ErrHelp) {
		fmt.Fprint(os.Stdout, cli.Usage())
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprint(os.Stderr, cli.Usage())
		os.Exit(2)
	}

	app.LoadDotEnv()
	if args.Token == "" {
		args.Token = app.GitHubToken()
	}
	if args.Token == "" {
		fatal("GITHUB_TOKEN is not set (or pass --token)")
	}

	analyzer, err := app.AnalyzerFromEnv()
	if err != nil {
		fatal(err.Error())
	}
	log.Printf("provider %s", analyzer.Name())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(os.Stderr, "Fetching %s\n", args.URL)
	job, err := analyze.Load(ctx, args.URL, args.Token)
	if err != nil {
		fatal(err.Error())
	}
	defer job.Close()

	title := fmt.Sprintf("%s/%s#%d", job.Ref.Owner, job.Ref.Repo, job.Ref.Number)
	fmt.Fprintf(os.Stderr, "Analyzing %s\n", title)

	var events []analysis.StreamEvent
	streamErr := job.Stream(ctx, analyzer, nil, func(ev analysis.StreamEvent) error {
		events = append(events, ev)
		if ev.Type == "category" {
			id, _ := ev.Data["id"].(string)
			risk, _ := ev.Data["riskLevel"].(string)
			if id != "" {
				fmt.Fprintf(os.Stderr, "  %s %s\n", id, risk)
			}
		}
		return nil
	})

	if args.JSON {
		if err := writeJSON(os.Stdout, events); err != nil {
			fatal(err.Error())
		}
	} else {
		fmt.Fprint(os.Stdout, cli.FormatReport(title, events))
	}
	if streamErr != nil {
		fatal(streamErr.Error())
	}

	if args.Post {
		if err := postReview(ctx, args.Token, job.Ref, args.URL, events); err != nil {
			fatal(err.Error())
		}
	}
}

func postReview(ctx context.Context, token string, ref *analysis.PRRef, url string, events []analysis.StreamEvent) error {
	action, reason := cli.Recommendation(events)
	event := cli.GitHubEvent(action)
	comments := cli.ReviewComments(events)
	fmt.Fprintf(os.Stderr, "Posting %s review (%d comments) to %s\n", event, len(comments), url)
	gh := github.NewClient(token)
	if err := gh.PostReview(ctx, ref, event, reason, comments); err != nil {
		return fmt.Errorf("post review: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Posted.")
	return nil
}

func writeJSON(w io.Writer, events []analysis.StreamEvent) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(events)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
