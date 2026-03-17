package ai

import (
	"strings"
)

// FilterDiffByFiles returns only the diff hunks for the given set of filenames.
// A hunk starts with "diff --git a/<file> b/<file>" and ends at the next such line.
func FilterDiffByFiles(diff string, filenames []string) string {
	if diff == "" || len(filenames) == 0 {
		return ""
	}

	wanted := make(map[string]bool, len(filenames))
	for _, f := range filenames {
		wanted[f] = true
	}

	var out strings.Builder
	var current strings.Builder
	var currentFile string

	flush := func() {
		if currentFile != "" && wanted[currentFile] {
			out.WriteString(current.String())
		}
		current.Reset()
		currentFile = ""
	}

	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			// Extract filename: "diff --git a/path/to/file.go b/path/to/file.go"
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// parts[3] is "b/path/to/file.go"
				currentFile = strings.TrimPrefix(parts[3], "b/")
			}
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	flush()

	return out.String()
}
