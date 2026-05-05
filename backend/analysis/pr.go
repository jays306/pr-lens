package analysis

// PRRef holds parsed pull request coordinates.
type PRRef struct {
	Owner  string
	Repo   string
	Number int
}

// PRFile represents a changed file in a pull request.
type PRFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"` // added, removed, modified, renamed
	Patch    string `json:"patch"`
}

// PRInfo holds metadata about the pull request.
type PRInfo struct {
	Base struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
}

// FileContent holds the full content of a file at a given ref.
type FileContent struct {
	Path    string
	Content string
}

// ExistingComment is feedback already posted on the PR. Anchor describes how
// precisely GitHub can attach it to the current diff.
type ExistingComment struct {
	ID           int    `json:"id,omitempty"`
	Path         string `json:"path,omitempty"`
	Line         int    `json:"line,omitempty"`
	OriginalLine int    `json:"originalLine,omitempty"`
	Author       string `json:"author"`
	Body         string `json:"body"`
	Anchor       string `json:"anchor"`            // inline, file, or pr
	Source       string `json:"source"`            // review_comment, review, or issue_comment
	Outdated     bool   `json:"outdated,omitempty"` // inline comment whose line no longer exists in current diff
	Resolved     bool   `json:"resolved,omitempty"` // true when the review thread this comment belongs to is resolved
}
