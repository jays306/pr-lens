package analysis

import "strings"

// FilterDiffByFiles returns only the diff hunks for the given set of filenames.
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

	for _, line := range strings.SplitAfter(diff, "\n") {
		trimmed := strings.TrimRight(line, "\n")
		if strings.HasPrefix(trimmed, "diff --git ") {
			flush()
			parts := strings.Fields(trimmed)
			if len(parts) >= 4 {
				currentFile = strings.TrimPrefix(parts[3], "b/")
			}
		}
		current.WriteString(line)
	}
	flush()

	return out.String()
}
