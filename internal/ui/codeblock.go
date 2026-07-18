package ui

// Fenced-code-block extraction for the ctrl+b "copy code block" key.

import "strings"

// lastCodeBlock returns the body of the final fenced code block in markdown,
// without the fence lines. A fence left unclosed (a reply stopped mid-block)
// counts and yields the partial body.
func lastCodeBlock(markdown string) (string, bool) {
	lines := strings.Split(markdown, "\n")
	var last string
	found := false
	for i := 0; i < len(lines); i++ {
		marker, size := fenceOpen(lines[i])
		if size == 0 {
			continue
		}
		j := i + 1
		for j < len(lines) && !fenceCloses(lines[j], marker, size) {
			j++
		}
		last = strings.Join(lines[i+1:j], "\n")
		found = true
		i = j
	}
	return last, found
}

// fenceOpen reports the marker and length of a code fence opening on line:
// at most 3 spaces of indent, then 3+ backticks or tildes (an info string
// may follow). size 0 means no fence.
func fenceOpen(line string) (marker byte, size int) {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 || s == "" || (s[0] != '`' && s[0] != '~') {
		return 0, 0
	}
	marker = s[0]
	for size < len(s) && s[size] == marker {
		size++
	}
	if size < 3 {
		return 0, 0
	}
	return marker, size
}

// fenceCloses reports whether line closes a fence of the given marker and
// minimum length: a run of at least size markers and nothing else.
func fenceCloses(line string, marker byte, size int) bool {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 {
		return false
	}
	n := 0
	for n < len(s) && s[n] == marker {
		n++
	}
	return n >= size && strings.TrimRight(s[n:], " \t") == ""
}
