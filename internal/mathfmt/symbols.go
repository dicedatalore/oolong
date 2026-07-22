package mathfmt

import "strings"

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
