package cli

import (
	"testing"
)

func TestParseArgs_URL(t *testing.T) {
	a, err := ParseArgs([]string{"https://github.com/acme/api/pull/9"})
	if err != nil {
		t.Fatal(err)
	}
	if a.URL != "https://github.com/acme/api/pull/9" {
		t.Fatalf("url=%q", a.URL)
	}
	if a.JSON || a.Post {
		t.Fatalf("flags should be off: %+v", a)
	}
}

func TestParseArgs_ReviewSubcommandAndFlags(t *testing.T) {
	a, err := ParseArgs([]string{"review", "acme/api#9", "--json", "--post", "--token", "ghp_x"})
	if err != nil {
		t.Fatal(err)
	}
	if a.URL != "https://github.com/acme/api/pull/9" {
		t.Fatalf("url=%q", a.URL)
	}
	if !a.JSON || !a.Post || a.Token != "ghp_x" {
		t.Fatalf("got %+v", a)
	}
}

func TestParseArgs_ShorthandRepoPR(t *testing.T) {
	a, err := ParseArgs([]string{"acme/api/12"})
	if err != nil {
		t.Fatal(err)
	}
	if a.URL != "https://github.com/acme/api/pull/12" {
		t.Fatalf("url=%q", a.URL)
	}
}

func TestParseArgs_MissingURL(t *testing.T) {
	if _, err := ParseArgs([]string{"--json"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseArgs_Help(t *testing.T) {
	_, err := ParseArgs([]string{"--help"})
	if err != ErrHelp {
		t.Fatalf("got %v", err)
	}
}
