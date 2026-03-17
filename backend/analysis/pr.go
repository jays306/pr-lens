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
