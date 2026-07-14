package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// renderMath replaces LaTeX math regions in assistant markdown with plain
// Unicode text before the message reaches glamour, which would otherwise eat
// the backslashes and show mangled LaTeX. Code blocks and inline code are
// left untouched, and an unclosed delimiter passes through literally.
func renderMath(markdown string) string {
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
				out.WriteString(displayBlock(latexToUnicode(string(src[i+2:end]))))
				i = end + 2
				break
			}
			out.WriteString(`\[`)
			i += 2
		case r == '\\' && i+1 < n && src[i+1] == '(':
			end := indexSeq(src, i+2, `\)`)
			if end >= 0 {
				out.WriteString(escapeMarkdown(latexToUnicode(string(src[i+2:end]))))
				i = end + 2
				break
			}
			out.WriteString(`\(`)
			i += 2
		case r == '$' && i+1 < n && src[i+1] == '$':
			end := indexSeq(src, i+2, "$$")
			if end >= 0 {
				out.WriteString(displayBlock(latexToUnicode(string(src[i+2:end]))))
				i = end + 2
				break
			}
			out.WriteString("$$")
			i += 2
		case r == '$':
			if end, ok := findInlineDollar(src, i); ok {
				out.WriteString(escapeMarkdown(latexToUnicode(string(src[i+1:end]))))
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

// ---- LaTeX → Unicode conversion ----

var symbols = map[string]string{
	// Greek, lowercase
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ",
	"epsilon": "ε", "varepsilon": "ε", "zeta": "ζ", "eta": "η",
	"theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ",
	"lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "omicron": "ο",
	"pi": "π", "varpi": "ϖ", "rho": "ρ", "varrho": "ϱ",
	"sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ",
	"phi": "φ", "varphi": "ϕ", "chi": "χ", "psi": "ψ", "omega": "ω",
	// Greek, uppercase
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ",
	"Pi": "Π", "Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ",
	"Omega": "Ω",
	// operators and relations
	"times": "×", "cdot": "·", "pm": "±", "mp": "∓", "div": "÷",
	"ast": "∗", "star": "⋆", "circ": "∘", "bullet": "•",
	"leq": "≤", "le": "≤", "geq": "≥", "ge": "≥", "neq": "≠", "ne": "≠",
	"approx": "≈", "sim": "∼", "simeq": "≃", "cong": "≅",
	"propto": "∝", "equiv": "≡", "ll": "≪", "gg": "≫",
	// calculus and big operators
	"sum": "∑", "prod": "∏", "int": "∫", "iint": "∬", "oint": "∮",
	"partial": "∂", "nabla": "∇", "infty": "∞",
	// arrows
	"to": "→", "rightarrow": "→", "leftarrow": "←", "gets": "←",
	"leftrightarrow": "↔", "Rightarrow": "⇒", "Leftarrow": "⇐",
	"Leftrightarrow": "⇔", "mapsto": "↦", "uparrow": "↑", "downarrow": "↓",
	// dots and misc
	"cdots": "…", "dots": "…", "ldots": "…", "vdots": "⋮",
	"degree": "°", "prime": "′", "hbar": "ℏ", "ell": "ℓ",
	"Re": "ℜ", "Im": "ℑ", "aleph": "ℵ", "wp": "℘", "angle": "∠",
	"emptyset": "∅", "varnothing": "∅",
	// sets and logic
	"in": "∈", "notin": "∉", "ni": "∋", "cup": "∪", "cap": "∩",
	"subset": "⊂", "supset": "⊃", "subseteq": "⊆", "supseteq": "⊇",
	"setminus": "∖", "forall": "∀", "exists": "∃", "neg": "¬",
	"land": "∧", "wedge": "∧", "lor": "∨", "vee": "∨",
	"oplus": "⊕", "otimes": "⊗", "perp": "⊥", "parallel": "∥",
	// named functions render as plain words
	"sin": "sin", "cos": "cos", "tan": "tan", "cot": "cot",
	"sec": "sec", "csc": "csc", "arcsin": "arcsin", "arccos": "arccos",
	"arctan": "arctan", "sinh": "sinh", "cosh": "cosh", "tanh": "tanh",
	"log": "log", "ln": "ln", "lg": "lg", "exp": "exp",
	"lim": "lim", "sup": "sup", "inf": "inf", "max": "max", "min": "min",
	"det": "det", "dim": "dim", "mod": "mod", "gcd": "gcd",
}

// spacedSymbols marks relations and binary operators that read better with
// surrounding spaces; collapseSpaces squeezes any doubling with source spaces.
var spacedSymbols = map[string]bool{
	"times": true, "cdot": true, "pm": true, "mp": true, "div": true,
	"ast": true, "star": true,
	"leq": true, "le": true, "geq": true, "ge": true, "neq": true, "ne": true,
	"approx": true, "sim": true, "simeq": true, "cong": true,
	"propto": true, "equiv": true, "ll": true, "gg": true,
	"to": true, "rightarrow": true, "leftarrow": true, "gets": true,
	"leftrightarrow": true, "Rightarrow": true, "Leftarrow": true,
	"Leftrightarrow": true, "mapsto": true,
	"in": true, "notin": true, "ni": true, "cup": true, "cap": true,
	"subset": true, "supset": true, "subseteq": true, "supseteq": true,
	"setminus": true, "land": true, "wedge": true, "lor": true, "vee": true,
	"oplus": true, "otimes": true,
}

var superscripts = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴',
	'5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '−': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
	'n': 'ⁿ', 'i': 'ⁱ',
	'∘': '°', // ^\circ is the usual LaTeX idiom for degrees
}

var subscripts = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄',
	'5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '−': '₋', '=': '₌', '(': '₍', ')': '₎',
	'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ', 'k': 'ₖ',
	'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ', 'p': 'ₚ', 'r': 'ᵣ',
	's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ', 'v': 'ᵥ', 'x': 'ₓ',
}

func toSuperscript(s string) (string, bool) {
	return mapRunes(s, superscripts)
}

func toSubscript(s string) (string, bool) {
	return mapRunes(s, subscripts)
}

func mapRunes(s string, table map[rune]rune) (string, bool) {
	if s == "" {
		return "", false
	}
	var b strings.Builder
	for _, r := range s {
		m, ok := table[r]
		if !ok {
			return "", false
		}
		b.WriteRune(m)
	}
	return b.String(), true
}

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
