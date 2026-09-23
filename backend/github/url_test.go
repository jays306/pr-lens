package github

import "testing"

func TestParsePRURL_Valid(t *testing.T) {
	ref, err := ParsePRURL("https://github.com/owner/repo/pull/123")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Owner != "owner" || ref.Repo != "repo" || ref.Number != 123 {
		t.Fatalf("got %+v", ref)
	}
}

func TestParsePRURL_WithFilesSuffix(t *testing.T) {
	ref, err := ParsePRURL("https://github.com/owner/repo/pull/9/files")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Number != 9 {
		t.Fatalf("got %+v", ref)
	}
}

func TestParsePRURL_RejectsEmbeddedHost(t *testing.T) {
	if _, err := ParsePRURL("https://evil.com/github.com/owner/repo/pull/1"); err == nil {
		t.Fatal("expected rejection of embedded github.com path")
	}
}

func TestParsePRURL_RejectsWrongHost(t *testing.T) {
	if _, err := ParsePRURL("https://gitlab.com/owner/repo/pull/1"); err == nil {
		t.Fatal("expected rejection of non-github host")
	}
}

func TestParsePRURL_RejectsInvalidName(t *testing.T) {
	if _, err := ParsePRURL("https://github.com/ow ner/repo/pull/1"); err == nil {
		t.Fatal("expected rejection of invalid owner")
	}
}
