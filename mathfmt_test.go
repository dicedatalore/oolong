package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func TestLatexToUnicode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`E=mc^2`, "E=mc²"},
		{`v_{\text{esc}} = \sqrt{\frac{2GM}{r}}`, "v_esc = √(2GM/r)"},
		{`x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`, "x = (−b ± √(b² − 4ac))/(2a)"},
		{`\sqrt[3]{x}`, "³√x"},
		{`\sqrt{2}`, "√2"},
		{`\alpha \cdot \beta \leq \gamma`, "α · β ≤ γ"},
		{`x_i^2`, "xᵢ²"},
		{`a_{max}`, "aₘₐₓ"},
		{`T_{eff}`, "T_eff"},
		{`e^{i\pi}`, "e^(iπ)"},
		{`\frac{\sqrt{a+b}}{c^{2}}`, "(√(a+b))/(c²)"},
		{`\left( \frac{a}{b} \right)^2`, "(a/b)²"},
		{`\mathfrak{g} + \foo`, `\mathfrak{g} + \foo`},
		{`\int_0^\infty e^{-x}\, dx`, "∫₀^∞ e^(−x) dx"},
		{`\sum_{n=1}^{\infty} \frac{1}{n^2} = \frac{\pi^2}{6}`, "∑ₙ₌₁^∞ 1/(n²) = π²/6"},
		{`\Delta E \approx \hbar \omega`, "Δ E ≈ ℏ ω"},
		{`a\cdot b\leq c`, "a · b ≤ c"},
		{`f(x) \to \infty`, "f(x) → ∞"},
		{`90^\circ`, "90°"},
	}
	for _, c := range cases {
		if got := latexToUnicode(c.in); got != c.want {
			t.Errorf("latexToUnicode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderMath(t *testing.T) {
	nbsp := "\u00a0\u00a0"
	cases := []struct {
		name, in, want string
	}{
		{
			"display brackets",
			`Velocity: \[ v=\sqrt{x} \] done.`,
			"Velocity: \n\n" + nbsp + "v=√x\n\n done.",
		},
		{
			"display dollars",
			`$$E=mc^2$$`,
			"\n\n" + nbsp + "E=mc²\n\n",
		},
		{
			"inline parens",
			`Then \(E=mc^2\) holds.`,
			"Then E=mc² holds.",
		},
		{
			"inline dollar",
			`Let $x_i^2$ be small.`,
			"Let xᵢ² be small.",
		},
		{
			"underscore fallback is escaped",
			`\(v_{eff}\)`,
			`v\_eff`,
		},
		{
			"money untouched",
			`That costs $5 and $10 today.`,
			`That costs $5 and $10 today.`,
		},
		{
			"fenced code untouched",
			"```\n\\[ x \\]\n$y$\n```\n",
			"```\n\\[ x \\]\n$y$\n```\n",
		},
		{
			"inline code untouched",
			"Use `$x$` or `\\[y\\]` here.",
			"Use `$x$` or `\\[y\\]` here.",
		},
		{
			"unclosed display untouched",
			`\[ x`,
			`\[ x`,
		},
		{
			"unclosed dollar untouched",
			`a $x here`,
			`a $x here`,
		},
	}
	for _, c := range cases {
		if got := renderMath(c.in); got != c.want {
			t.Errorf("%s: renderMath(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestRenderMathThroughGlamour(t *testing.T) {
	reply := "The escape velocity is\n\n" +
		`\[ v_{\text{esc}} = \sqrt{\frac{2GM}{r}} \]` +
		"\n\nand kinetic energy is \\(E = \\tfrac{1}{2}mv^2\\)."

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styles.LightStyle),
		glamour.WithWordWrap(78),
	)
	if err != nil {
		t.Fatalf("building renderer: %v", err)
	}
	rendered, err := renderer.Render(renderMath(reply))
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	out := stripAnsi(rendered)

	for _, want := range []string{"v_esc = √(2GM/r)", "mv²"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q\noutput:\n%s", want, out)
		}
	}
	for _, bad := range []string{`\[`, `\sqrt`, `[ v_`} {
		if strings.Contains(out, bad) {
			t.Errorf("rendered output still contains %q\noutput:\n%s", bad, out)
		}
	}
}
