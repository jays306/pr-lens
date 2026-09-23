package handler

import (
	"net/http"
	"testing"
)

func TestResolveGitHubToken_BearerWins(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/analyze", nil)
	r.Header.Set("Authorization", "Bearer client-token")
	r.Header.Set("X-GitHub-Token", "header-token")
	got, err := ResolveGitHubToken(r, "server-token", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "client-token" {
		t.Fatalf("got %q, want client-token", got)
	}
}

func TestResolveGitHubToken_HeaderFallback(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/analyze", nil)
	r.Header.Set("X-GitHub-Token", "header-token")
	got, err := ResolveGitHubToken(r, "server-token", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "header-token" {
		t.Fatalf("got %q, want header-token", got)
	}
}

func TestResolveGitHubToken_ServerDefaultAllowed(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/analyze", nil)
	got, err := ResolveGitHubToken(r, "server-token", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "server-token" {
		t.Fatalf("got %q, want server-token", got)
	}
}

func TestResolveGitHubToken_ServerDefaultRejected(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/review", nil)
	_, err := ResolveGitHubToken(r, "server-token", false)
	if err == nil {
		t.Fatal("expected error when server default is not allowed")
	}
}

func TestResolveGitHubToken_EmptyBearerIgnored(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/review", nil)
	r.Header.Set("Authorization", "Bearer   ")
	_, err := ResolveGitHubToken(r, "server-token", false)
	if err == nil {
		t.Fatal("expected error for empty bearer")
	}
}
