package mathfmt

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// latexToUnicode converts one math region to plain Unicode text. Unknown
// commands pass through verbatim so output is never worse than the input.
func latexToUnicode(expr string) string {
	p := &texParser{src: []rune(expr)}
	out := collapseSpaces(p.parseSequence(0))
	out = strings.ReplaceAll(out, "( ", "(")
	out = strings.ReplaceAll(out, " )", ")")
	return strings.TrimSpace(out)
}

type texParser struct {
	src []rune
	pos int
}

// parseSequence consumes runes until the stop rune (0 = end of input),
// leaving the stop rune unconsumed.
func (p *texParser) parseSequence(stop rune) string {
	var b strings.Builder
	for p.pos < len(p.src) {
		r := p.src[p.pos]
		if stop != 0 && r == stop {
			break
		}
		switch r {
		case '\\':
			p.pos++
			b.WriteString(p.parseCommand())
		case '^':
			p.pos++
			arg := p.parseArg()
			if sup, ok := toSuperscript(arg); ok {
				b.WriteString(sup)
			} else if utf8.RuneCountInString(arg) == 1 {
				b.WriteString("^" + arg)
			} else {
				b.WriteString("^(" + arg + ")")
			}
		case '_':
			p.pos++
			arg := p.parseArg()
			if sub, ok := toSubscript(arg); ok {
				b.WriteString(sub)
			} else if isWordLike(arg) {
				b.WriteString("_" + arg)
			} else {
				b.WriteString("_(" + arg + ")")
			}
		case '{':
			b.WriteString(p.parseGroup())
		case '}':
			p.pos++ // stray unbalanced brace
		case '~':
			b.WriteRune(' ')
			p.pos++
		case '-':
			b.WriteRune('−')
			p.pos++
		default:
			b.WriteRune(r)
			p.pos++
		}
	}
	return b.String()
}

// parseCommand handles the token after a backslash (p.pos is past the '\').
func (p *texParser) parseCommand() string {
	if p.pos >= len(p.src) {
		return `\`
	}
	r := p.src[p.pos]
	if !unicode.IsLetter(r) {
		p.pos++
		switch r {
		case '{', '}', '%', '$', '_', '&', '#', '|':
			return string(r)
		case '\\':
			return "\n"
		case ',', ';', ':', '!':
			return ""
		case ' ':
			return " "
		default:
			return `\` + string(r)
		}
	}
	start := p.pos
	for p.pos < len(p.src) && unicode.IsLetter(p.src[p.pos]) {
		p.pos++
	}
	name := string(p.src[start:p.pos])
	if s, ok := symbols[name]; ok {
		if spacedSymbols[name] {
			return " " + s + " "
		}
		return s
	}
	switch name {
	case "frac", "dfrac", "tfrac":
		num := p.parseArg()
		den := p.parseArg()
		return wrapNumerator(num) + "/" + wrapDenominator(den)
	case "sqrt":
		idx := ""
		p.skipSpaces()
		if p.pos < len(p.src) && p.src[p.pos] == '[' {
			p.pos++
			idx = p.parseSequence(']')
			if p.pos < len(p.src) && p.src[p.pos] == ']' {
				p.pos++
			}
		}
		arg := p.parseArg()
		radical := "√"
		if idx != "" {
			if sup, ok := toSuperscript(idx); ok {
				radical = sup + "√"
			} else {
				radical = "(" + idx + ")√"
			}
		}
		if utf8.RuneCountInString(arg) == 1 {
			return radical + arg
		}
		return radical + "(" + arg + ")"
	case "text", "textrm", "textbf", "textit", "texttt",
		"mathrm", "mathbf", "mathit", "mathcal", "mathbb", "mathtt",
		"mathsf", "operatorname", "boldsymbol", "bm":
		return p.parseArg()
	case "left", "right":
		p.skipSpaces()
		if p.pos >= len(p.src) {
			return ""
		}
		d := p.src[p.pos]
		if d == '\\' && p.pos+1 < len(p.src) {
			p.pos += 2
			switch p.src[p.pos-1] {
			case '|':
				return "‖"
			default:
				return string(p.src[p.pos-1])
			}
		}
		p.pos++
		if d == '.' {
			return ""
		}
		return string(d)
	case "quad", "qquad", "space":
		return " "
	case "limits", "nolimits", "displaystyle", "textstyle":
		return ""
	default:
		// unknown command: keep it verbatim, including one brace group so
		// \mathfrak{g} survives as-is instead of mashing into "\mathfrakg"
		if p.pos < len(p.src) && p.src[p.pos] == '{' {
			return `\` + name + "{" + p.parseGroup() + "}"
		}
		return `\` + name
	}
}

// parseArg returns the next argument: a {...} group, a \command result, or a
// single rune (so ^2 and _i work without braces).
func (p *texParser) parseArg() string {
	p.skipSpaces()
	if p.pos >= len(p.src) {
		return ""
	}
	switch p.src[p.pos] {
	case '{':
		return p.parseGroup()
	case '\\':
		p.pos++
		return p.parseCommand()
	}
	r := p.src[p.pos]
	p.pos++
	if r == '-' {
		return "−"
	}
	return string(r)
}

func (p *texParser) parseGroup() string {
	p.pos++ // consume '{'
	s := p.parseSequence('}')
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
	}
	return s
}

func (p *texParser) skipSpaces() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

const operatorRunes = " +−±∓×·÷/=<>≤≥≠≈∼"

func wrapNumerator(s string) string {
	if s == "" || isParenWrapped(s) || !strings.ContainsAny(s, operatorRunes) {
		return s
	}
	return "(" + s + ")"
}

func wrapDenominator(s string) string {
	if utf8.RuneCountInString(s) <= 1 || isParenWrapped(s) {
		return s
	}
	return "(" + s + ")"
}

// isParenWrapped reports whether s is a single balanced (...) group.
func isParenWrapped(s string) bool {
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return false
	}
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return false
			}
		}
	}
	return depth == 0
}

func isWordLike(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// collapseSpaces squeezes runs of spaces (LaTeX ignores whitespace runs).
func collapseSpaces(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
