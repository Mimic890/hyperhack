package main

import (
	"os"
	"strings"
	"testing"
)

// TestReadmeCaddyBlock keeps the Caddy block in both READMEs equal to Caddyfile.example
// without its leading comment.
func TestReadmeCaddyBlock(t *testing.T) {
	example, err := os.ReadFile("Caddyfile.example")
	if err != nil {
		t.Fatal(err)
	}
	want := string(example)
	for strings.HasPrefix(want, "#") || strings.HasPrefix(want, "\n") {
		_, want, _ = strings.Cut(want, "\n")
	}
	if want == "" {
		t.Fatal("Caddyfile.example has no config after its comment")
	}
	for _, name := range []string{"README.md", "README.ru.md"} {
		readme, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		blocks := strings.Split(string(readme), "```caddyfile\n")
		if len(blocks) != 2 {
			t.Errorf("%s: want exactly one caddyfile block, got %d", name, len(blocks)-1)
			continue
		}
		got, _, ok := strings.Cut(blocks[1], "```")
		if !ok {
			t.Errorf("%s: caddyfile block not closed", name)
			continue
		}
		if got != want {
			t.Errorf("%s: caddyfile block differs from Caddyfile.example; copy it from there", name)
		}
	}
}
