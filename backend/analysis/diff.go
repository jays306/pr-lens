package analysis

import (
	"regexp"
	"strings"
)

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
			currentFile = parseDiffGitPath(trimmed)
		}
		current.WriteString(line)
	}
	flush()

	return out.String()
}

// parseDiffGitPath extracts the destination path from a `diff --git a/X b/Y`
// header. Using the ` b/` marker rather than Fields keeps paths that contain
// spaces.
func parseDiffGitPath(header string) string {
	rest := strings.TrimPrefix(header, "diff --git ")
	idx := strings.Index(rest, " b/")
	if idx < 0 {
		idx = strings.LastIndex(rest, " b/")
	}
	if idx < 0 {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(rest[idx:]), "b/")
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// DiffAnchor is a GitHub review-comment target: a file line on LEFT (deleted)
// or RIGHT (added / still-present) of the pull request diff.
type DiffAnchor struct {
	Path string
	Line int
	Side string
	OK   bool
}

type diffLine struct {
	oldLine int
	newLine int
	kind    byte // ' ', '+', '-'
	text    string
	hunk    int
}

// DiffIndex maps destination file paths to parsed unified-diff lines.
type DiffIndex map[string][]diffLine

// ParseDiffIndex walks a unified diff and records every context, added, and
// deleted line with its old/new file numbers.
func ParseDiffIndex(diff string) DiffIndex {
	idx := make(DiffIndex)
	if diff == "" {
		return idx
	}

	var file string
	var oldLine, newLine, hunk int
	inHunk := false

	for _, raw := range strings.Split(diff, "\n") {
		line := strings.TrimRight(raw, "\r")
		switch {
		case strings.HasPrefix(line, "diff --git "):
			file = parseDiffGitPath(line)
			inHunk = false
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
			continue
		case strings.HasPrefix(line, "@@"):
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil || file == "" {
				inHunk = false
				continue
			}
			oldLine = atoi(m[1])
			newLine = atoi(m[2])
			hunk++
			inHunk = true
		case !inHunk || file == "":
			continue
		case line == "\\ No newline at end of file":
			continue
		case strings.HasPrefix(line, "+"):
			idx[file] = append(idx[file], diffLine{
				newLine: newLine, kind: '+', text: line[1:], hunk: hunk,
			})
			newLine++
		case strings.HasPrefix(line, "-"):
			idx[file] = append(idx[file], diffLine{
				oldLine: oldLine, kind: '-', text: line[1:], hunk: hunk,
			})
			oldLine++
		default:
			text := line
			if strings.HasPrefix(line, " ") {
				text = line[1:]
			}
			idx[file] = append(idx[file], diffLine{
				oldLine: oldLine, newLine: newLine, kind: ' ', text: text, hunk: hunk,
			})
			oldLine++
			newLine++
		}
	}
	return idx
}

// InferSnippetAnchor finds the first changed line that matches the snippet
// text. Added lines win over hunk-header / context line numbers so comments
// attach to the actual change.
func InferSnippetAnchor(idx DiffIndex, path, after, before string, claimed int) DiffAnchor {
	canonical, lines := idx.resolvePath(path)
	if len(lines) == 0 {
		if claimed > 0 {
			return DiffAnchor{Path: path, Line: claimed, Side: "RIGHT", OK: true}
		}
		return DiffAnchor{}
	}

	if needle := firstMeaningfulLine(after); needle != "" {
		if a, ok := matchText(lines, needle, '+', claimed); ok {
			a.Path = canonical
			return a
		}
	}
	if needle := firstMeaningfulLine(before); needle != "" {
		if a, ok := matchText(lines, needle, '-', claimed); ok {
			a.Path = canonical
			return a
		}
	}
	return SnapCommentAnchor(idx, path, claimed)
}

// SnapCommentAnchor maps a requested file line onto a line GitHub will accept
// on this diff. Context-only targets are moved to the nearest addition in the
// same hunk so comments land on the change, not the hunk header.
func SnapCommentAnchor(idx DiffIndex, path string, line int) DiffAnchor {
	canonical, lines := idx.resolvePath(path)
	if len(lines) == 0 {
		if line > 0 {
			return DiffAnchor{Path: path, Line: line, Side: "RIGHT", OK: true}
		}
		return DiffAnchor{}
	}

	var claimedHunk int
	var add, del, ctx *diffLine
	for i := range lines {
		l := &lines[i]
		if l.kind == '+' && l.newLine == line {
			add = l
		}
		if l.kind == '-' && l.oldLine == line {
			del = l
		}
		if l.kind == ' ' && l.newLine == line {
			ctx = l
		}
	}
	if add != nil {
		return DiffAnchor{Path: canonical, Line: add.newLine, Side: "RIGHT", OK: true}
	}
	if del != nil && ctx == nil {
		return DiffAnchor{Path: canonical, Line: del.oldLine, Side: "LEFT", OK: true}
	}
	if ctx != nil {
		claimedHunk = ctx.hunk
	} else if del != nil {
		claimedHunk = del.hunk
	}

	if nearby, ok := nearestOfKind(lines, '+', line, claimedHunk); ok {
		nearby.Path = canonical
		return nearby
	}
	if del != nil {
		return DiffAnchor{Path: canonical, Line: del.oldLine, Side: "LEFT", OK: true}
	}
	if ctx != nil {
		return DiffAnchor{Path: canonical, Line: ctx.newLine, Side: "RIGHT", OK: true}
	}
	if del, ok := nearestOfKind(lines, '-', line, claimedHunk); ok {
		del.Path = canonical
		return del
	}
	if ctx, ok := nearestOfKind(lines, ' ', line, claimedHunk); ok {
		ctx.Path = canonical
		return ctx
	}
	if line > 0 {
		return DiffAnchor{Path: canonical, Line: line, Side: "RIGHT", OK: true}
	}
	return DiffAnchor{}
}

// AnchorFromBody picks the changed line whose code best matches the comment
// text (backticked identifiers, names). Falls back to SnapCommentAnchor when
// nothing in the body maps onto the diff — that is the case that used to pin
// comments to the hunk header.
func AnchorFromBody(idx DiffIndex, path string, claimed int, body string) DiffAnchor {
	fallback := SnapCommentAnchor(idx, path, claimed)
	needles := extractNeedles(body)
	if len(needles) == 0 {
		return fallback
	}
	canonical, lines := idx.resolvePath(path)
	if len(lines) == 0 {
		return fallback
	}

	bigrams := extractBigrams(body)
	focus := focusVersions(body)
	rare := rareCodeNeedles(lines, needles)
	required := onlyIdents(body)
	requireOnly := false
	if len(required) > 0 {
		for _, l := range lines {
			if lineHasAll(l.text, required) {
				requireOnly = true
				break
			}
		}
	}
	// When the comment names two module versions, pin to the last one so a
	// nearby checksum for the pinned version does not win.
	lastVersion := ""
	if vers := versionRe.FindAllString(body, -1); len(vers) >= 2 {
		lastVersion = vers[len(vers)-1]
	}
	requireVersion := false
	if lastVersion != "" {
		for _, l := range lines {
			if tokenIn(l.text, lastVersion) {
				requireVersion = true
				break
			}
		}
	}
	bestScore := 0
	best := DiffAnchor{}
	bestKind := byte(0)
	for _, l := range lines {
		if l.kind != '+' && l.kind != '-' && l.kind != ' ' {
			continue
		}
		if requireOnly && !lineHasAll(l.text, required) {
			continue
		}
		if requireVersion && !tokenIn(l.text, lastVersion) {
			continue
		}
		score := scoreNeedles(l.text, needles) + scoreBigrams(l.text, bigrams) + scoreFocusVersions(l.text, focus) + scoreRareNeedles(l.text, rare)
		if score == 0 {
			continue
		}
		line, side := l.newLine, "RIGHT"
		if l.kind == '-' {
			line, side = l.oldLine, "LEFT"
		}
		better := !best.OK || score > bestScore
		if !better && score == bestScore {
			if kindPreferFor(l.kind, mentionsRemoval(body)) > kindPreferFor(bestKind, mentionsRemoval(body)) {
				better = true
			} else if l.kind == bestKind && abs(line-claimed) < abs(best.Line-claimed) {
				better = true
			}
		}
		if better {
			best = DiffAnchor{Path: canonical, Line: line, Side: side, OK: true}
			bestScore = score
			bestKind = l.kind
		}
	}
	if best.OK && bestScore >= 10 {
		if bestKind == '-' && !mentionsRemoval(body) {
			if add := bestOfKinds(lines, canonical, needles, bigrams, claimed, '+', ' '); add.OK {
				return add
			}
		}
		return best
	}
	return fallback
}

func mentionsRemoval(body string) bool {
	s := strings.ToLower(body)
	for _, w := range []string{"removed", "deleted", "dropped", "no longer"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func bestOfKinds(lines []diffLine, canonical string, needles []string, bigrams [][2]string, claimed int, kinds ...byte) DiffAnchor {
	best := DiffAnchor{}
	bestScore := 0
	for _, l := range lines {
		ok := false
		for _, k := range kinds {
			if l.kind == k {
				ok = true
				break
			}
		}
		if !ok {
			continue
		}
		score := scoreNeedles(l.text, needles) + scoreBigrams(l.text, bigrams)
		if score < 10 || score < bestScore {
			continue
		}
		line := l.newLine
		if line <= 0 {
			continue
		}
		if !best.OK || score > bestScore || abs(line-claimed) < abs(best.Line-claimed) {
			best = DiffAnchor{Path: canonical, Line: line, Side: "RIGHT", OK: true}
			bestScore = score
		}
	}
	return best
}

// kindPrefer ranks which diff line wins a tie: an addition, then unchanged
// context inside the hunk, then a deletion.
func kindPrefer(k byte) int {
	return kindPreferFor(k, false)
}

// kindPreferFor inverts the tie when the comment is about a removal, so a
// deleted flag beats another function that only shares a weak token.
func kindPreferFor(k byte, removal bool) int {
	switch k {
	case '+':
		if removal {
			return 1
		}
		return 2
	case '-':
		if removal {
			return 2
		}
		return 0
	case ' ':
		return 1
	default:
		return 0
	}
}

// SnippetLineEnd is the last added (or deleted) line in path whose text appears
// in after/before, used as GitHub's multi-line comment end.
func SnippetLineEnd(idx DiffIndex, path, after, before string, start int) int {
	_, lines := idx.resolvePath(path)
	if len(lines) == 0 {
		return start
	}
	wanted := make(map[string]bool)
	for _, src := range []string{after, before} {
		for _, line := range strings.Split(src, "\n") {
			if t := strings.TrimSpace(line); t != "" {
				wanted[t] = true
			}
		}
	}
	end := start
	for _, l := range lines {
		text := strings.TrimSpace(l.text)
		if text == "" || !wanted[text] {
			continue
		}
		line := l.newLine
		if l.kind == '-' {
			line = l.oldLine
		}
		if line > end {
			end = line
		}
	}
	return end
}

var (
	backtickRe     = regexp.MustCompile("`([^`]+)`")
	identRe        = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}`)
	versionRe      = regexp.MustCompile(`v\d+\.\d+\.\d+`)
	focusVersionRe = regexp.MustCompile(`(?i)\b(?:stale|unused|extra|duplicate)\b(?:\W+\w+){0,4}\W+(v\d+\.\d+\.\d+)`)
	onlyRe         = regexp.MustCompile(`(?i)\bonly\b`)
)

func extractNeedles(body string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, m := range identRe.FindAllString(s, -1) {
			if stopNeedles[strings.ToLower(m)] || seen[strings.ToLower(m)] {
				continue
			}
			seen[strings.ToLower(m)] = true
			out = append(out, m)
		}
	}
	for _, m := range backtickRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	stripped := backtickRe.ReplaceAllString(body, " ")
	for _, m := range identRe.FindAllString(stripped, -1) {
		if len(m) < 5 {
			continue
		}
		add(m)
	}
	for _, m := range versionRe.FindAllString(body, -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// focusVersions is the module version the comment calls stale. That token
// outranks a nearby checksum for a different version of another module.
func focusVersions(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range focusVersionRe.FindAllStringSubmatch(body, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}

func scoreFocusVersions(line string, versions []string) int {
	score := 0
	for _, v := range versions {
		if tokenIn(line, v) {
			score += 40
		}
	}
	return score
}

// onlyIdents is the identifier after "only", so "Comparing only Region" must
// land on a line that contains Region and not on the test-case name.
func onlyIdents(body string) []string {
	loc := onlyRe.FindStringIndex(body)
	if loc == nil {
		return nil
	}
	rest := body[loc[1]:]
	if len(rest) > 80 {
		rest = rest[:80]
	}
	for _, m := range identRe.FindAllString(rest, 6) {
		if stopNeedles[strings.ToLower(m)] {
			continue
		}
		return []string{m}
	}
	return nil
}

// rareCodeNeedles are camel-case or long identifiers that occur on exactly one
// diff line. They outrank a repeated token such as LocalStack that also
// appears on a different function.
func rareCodeNeedles(lines []diffLine, needles []string) []string {
	var out []string
	for _, n := range needles {
		if !codeLikeNeedle(n) {
			continue
		}
		hits := 0
		for _, l := range lines {
			if tokenIn(l.text, n) {
				hits++
			}
		}
		if hits == 1 {
			out = append(out, n)
		}
	}
	return out
}

func codeLikeNeedle(n string) bool {
	if len(n) >= 12 {
		return true
	}
	for _, r := range n[1:] {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func scoreRareNeedles(line string, rare []string) int {
	score := 0
	for _, n := range rare {
		if tokenIn(line, n) {
			score += 30
		}
	}
	return score
}

func lineHasAll(line string, idents []string) bool {
	for _, id := range idents {
		if !tokenIn(line, id) {
			return false
		}
	}
	return true
}

var stopNeedles = map[string]bool{
	"this": true, "that": true, "with": true, "from": true, "have": true,
	"been": true, "will": true, "when": true, "what": true, "which": true,
	"their": true, "there": true, "about": true, "would": true, "could": true,
	"should": true, "still": true, "while": true, "where": true, "after": true,
	"before": true, "because": true, "return": true, "returns": true,
	"error": true, "true": true, "false": true, "null": true, "func": true,
	"type": true, "string": true, "the": true, "and": true, "for": true,
}

func extractBigrams(body string) [][2]string {
	stripped := backtickRe.ReplaceAllString(body, " $1 ")
	toks := identRe.FindAllString(stripped, -1)
	var out [][2]string
	for i := 0; i+1 < len(toks); i++ {
		a, b := toks[i], toks[i+1]
		if stopNeedles[strings.ToLower(a)] || stopNeedles[strings.ToLower(b)] {
			continue
		}
		out = append(out, [2]string{a, b})
	}
	return out
}

func scoreBigrams(line string, bigrams [][2]string) int {
	score := 0
	for _, b := range bigrams {
		if tokenIn(line, b[0]) && tokenIn(line, b[1]) {
			score += 25
		}
	}
	return score
}

func scoreNeedles(line string, needles []string) int {
	matched := 0
	score := 0
	for _, n := range needles {
		if !tokenIn(line, n) {
			continue
		}
		matched++
		score += 10
		if len(n) >= 6 {
			score += 5
		}
	}
	if matched >= 2 {
		score += 15
	}
	return score
}

func tokenIn(line, needle string) bool {
	if needle == "" {
		return false
	}
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(needle) + `([^A-Za-z0-9_]|$)`)
	return re.MatchString(line)
}

func (idx DiffIndex) resolvePath(path string) (string, []diffLine) {
	if path == "" {
		return "", nil
	}
	if lines, ok := idx[path]; ok {
		return path, lines
	}
	var matches []string
	for p := range idx {
		if strings.HasSuffix(p, "/"+path) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 1 {
		return matches[0], idx[matches[0]]
	}
	return path, nil
}

func matchText(lines []diffLine, needle string, kind byte, claimed int) (DiffAnchor, bool) {
	best := DiffAnchor{}
	bestDist := int(^uint(0) >> 1)
	for _, l := range lines {
		if l.kind != kind || strings.TrimSpace(l.text) != needle {
			continue
		}
		line, side := l.newLine, "RIGHT"
		if kind == '-' {
			line, side = l.oldLine, "LEFT"
		}
		dist := abs(line - claimed)
		if claimed <= 0 {
			return DiffAnchor{Line: line, Side: side, OK: true}, true
		}
		if dist < bestDist {
			bestDist = dist
			best = DiffAnchor{Line: line, Side: side, OK: true}
		}
	}
	return best, best.OK
}

func nearestOfKind(lines []diffLine, kind byte, target, hunk int) (DiffAnchor, bool) {
	best := DiffAnchor{}
	bestDist := int(^uint(0) >> 1)
	bestSameHunk := false
	for _, l := range lines {
		if l.kind != kind {
			continue
		}
		line, side := l.newLine, "RIGHT"
		if kind == '-' {
			line, side = l.oldLine, "LEFT"
		}
		if line <= 0 {
			continue
		}
		dist := abs(line - target)
		sameHunk := hunk > 0 && l.hunk == hunk
		better := !best.OK || (sameHunk && !bestSameHunk) || (sameHunk == bestSameHunk && dist < bestDist)
		if better {
			best = DiffAnchor{Line: line, Side: side, OK: true}
			bestDist = dist
			bestSameHunk = sameHunk
		}
	}
	return best, best.OK
}

func firstMeaningfulLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}
