package ui

import "testing"

func TestLastCodeBlock(t *testing.T) {
	tests := []struct {
		name  string
		md    string
		want  string
		found bool
	}{
		{
			name:  "single block",
			md:    "Use this:\n\n```go\nfmt.Println(\"hi\")\n```\n",
			want:  "fmt.Println(\"hi\")",
			found: true,
		},
		{
			name:  "last of several",
			md:    "```\nfirst\n```\n\ntext\n\n```py\nsecond\n```\n",
			want:  "second",
			found: true,
		},
		{
			name:  "tilde fence",
			md:    "~~~\nbody\n~~~\n",
			want:  "body",
			found: true,
		},
		{
			name:  "no block",
			md:    "just `inline code` here",
			found: false,
		},
		{
			name:  "unclosed fence keeps the partial body",
			md:    "```go\npartial line",
			want:  "partial line",
			found: true,
		},
		{
			name:  "backtick block containing a tilde fence",
			md:    "````md\n~~~\ninner\n~~~\n````\n",
			want:  "~~~\ninner\n~~~",
			found: true,
		},
		{
			name:  "closing fence must be at least as long",
			md:    "````\nbody\n```\nstill body\n````\n",
			want:  "body\n```\nstill body",
			found: true,
		},
		{
			name:  "indented four spaces is not a fence",
			md:    "    ```\n    not a fence\n",
			found: false,
		},
		{
			name:  "empty block",
			md:    "```\n```\n",
			want:  "",
			found: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := lastCodeBlock(tt.md)
			if found != tt.found {
				t.Fatalf("found = %v, want %v", found, tt.found)
			}
			if found && got != tt.want {
				t.Errorf("body = %q, want %q", got, tt.want)
			}
		})
	}
}
