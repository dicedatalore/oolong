// Package mathfmt converts LaTeX math in markdown to plain Unicode text.
package mathfmt

import (
	"strings"
	"unicode"
)

// Render replaces LaTeX math regions in assistant markdown with plain
// Unicode text before the message reaches glamour, which would otherwise eat
// the backslashes and show mangled LaTeX. Code blocks and inline code are
// left untouched, and an unclosed delimiter passes through literally.
func Render(markdown string) string {
	src := []rune(markdown)
	n := len(src)
	var out strings.Builder
	atLineStart := true
	i := 0
	for i < n {
		if atLineStart {
			if end, ok := fenceSpan(src, i); ok {
				out.WriteString(string(src[i:end]))
				i = end
				continue
			}
		}
		r := src[i]
		switch {
		case r == '\n':
			out.WriteRune(r)
			i++
			atLineStart = true
			continue
		case r == '`':
			k := i
			for k < n && src[k] == '`' {
				k++
			}
			if end := findBacktickRun(src, k, k-i); end >= 0 {
				out.WriteString(string(src[i:end]))
				i = end
			} else {
				out.WriteString(string(src[i:k]))
				i = k
			}
		case r == '\\' && i+1 < n && src[i+1] == '[':
			end := indexSeq(src, i+2, `\]`)
			if end >= 0 {
				out.WriteString(displayBlock(latexToUnicode(string(src[i+2 : end]))))
				i = end + 2
				break
			}
			out.WriteString(`\[`)
			i += 2
		case r == '\\' && i+1 < n && src[i+1] == '(':
			end := indexSeq(src, i+2, `\)`)
			if end >= 0 {
				out.WriteString(escapeMarkdown(latexToUnicode(string(src[i+2 : end]))))
				i = end + 2
				break
			}
			out.WriteString(`\(`)
			i += 2
		case r == '$' && i+1 < n && src[i+1] == '$':
			end := indexSeq(src, i+2, "$$")
			if end >= 0 {
				out.WriteString(displayBlock(latexToUnicode(string(src[i+2 : end]))))
				i = end + 2
				break
			}
			out.WriteString("$$")
			i += 2
		case r == '$':
			if end, ok := findInlineDollar(src, i); ok {
				out.WriteString(escapeMarkdown(latexToUnicode(string(src[i+1 : end]))))
				i = end + 1
				break
			}
			out.WriteRune(r)
			i++
		case r == '\\' && i+1 < n:
			// markdown escape (e.g. \$) — copy the pair so the next rune is
			// never mistaken for a math opener
			out.WriteRune(src[i])
			out.WriteRune(src[i+1])
			i += 2
		default:
			out.WriteRune(r)
			i++
		}
		atLineStart = false
	}
	return collapseBlankLines(out.String())
}

// fenceSpan reports the extent of a fenced code block opening at line start i.
func fenceSpan(src []rune, i int) (int, bool) {
	n := len(src)
	j := i
	for spaces := 0; j < n && src[j] == ' ' && spaces < 3; spaces++ {
		j++
	}
	if j >= n || (src[j] != '`' && src[j] != '~') {
		return 0, false
	}
	marker := src[j]
	k := j
	for k < n && src[k] == marker {
		k++
	}
	if k-j < 3 {
		return 0, false
	}
	return findFenceEnd(src, k, marker, k-j), true
}

// findFenceEnd scans line by line for a closing fence of at least minLen
// marker runes and returns the index just past it (or len(src)).
func findFenceEnd(src []rune, from int, marker rune, minLen int) int {
	n := len(src)
	i := from
	for i < n && src[i] != '\n' {
		i++
	}
	if i < n {
		i++
	}
	for i < n {
		lineStart := i
		j := i
		for spaces := 0; j < n && src[j] == ' ' && spaces < 3; spaces++ {
			j++
		}
		k := j
		for k < n && src[k] == marker {
			k++
		}
		rest := k
		for rest < n && (src[rest] == ' ' || src[rest] == '\t') {
			rest++
		}
		if k-j >= minLen && (rest >= n || src[rest] == '\n') {
			if rest < n {
				rest++
			}
			return rest
		}
		i = lineStart
		for i < n && src[i] != '\n' {
			i++
		}
		if i < n {
			i++
		}
	}
	return n
}

// findBacktickRun finds a run of exactly runLen backticks (the CommonMark
// closing rule for code spans) and returns the index just past it, or -1.
func findBacktickRun(src []rune, from, runLen int) int {
	n := len(src)
	for i := from; i < n; {
		if src[i] != '`' {
			i++
			continue
		}
		j := i
		for j < n && src[j] == '`' {
			j++
		}
		if j-i == runLen {
			return j
		}
		i = j
	}
	return -1
}

func indexSeq(src []rune, from int, lit string) int {
	want := []rune(lit)
	for i := from; i+len(want) <= len(src); i++ {
		match := true
		for k, w := range want {
			if src[i+k] != w {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// findInlineDollar locates the closer of a conservative $...$ span: no space
// after the opener, closer on the same line, no space before the closer, and
// no digit after it (so "$5 and $10" stays literal money).
func findInlineDollar(src []rune, i int) (int, bool) {
	n := len(src)
	if i+1 >= n || src[i+1] == ' ' || src[i+1] == '\n' || src[i+1] == '$' {
		return 0, false
	}
	for j := i + 2; j < n && src[j] != '\n'; j++ {
		if src[j] != '$' || src[j-1] == ' ' || src[j-1] == '\\' {
			continue
		}
		if j+1 < n && unicode.IsDigit(src[j+1]) {
			continue
		}
		return j, true
	}
	return 0, false
}

// displayBlock formats converted display math as its own paragraph. The
// leading U+00A0 pair survives goldmark (ordinary leading spaces would be
// stripped) and renders as a two-space indent.
func displayBlock(s string) string {
	s = escapeMarkdown(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "\u00a0\u00a0" + strings.TrimSpace(l)
	}
	return "\n\n" + strings.Join(lines, "\\\n") + "\n\n"
}

var mdEscaper = strings.NewReplacer(
	`\`, `\\`,
	"_", `\_`,
	"*", `\*`,
	"`", "\\`",
	"[", `\[`,
	"]", `\]`,
)

func escapeMarkdown(s string) string {
	return mdEscaper.Replace(s)
}

func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
