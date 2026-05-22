package main

import (
	"strings"
	"testing"
)

// Each embedded script should be non-empty and mention the binary name so
// `source <(hopnet completion X)` actually wires completion to `hopnet`.
func TestCompletionEmbedsNamedScripts(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"bash", completionBash},
		{"zsh", completionZsh},
		{"fish", completionFish},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.body == "" {
				t.Fatalf("%s script embedded as empty string", tt.name)
			}
			if !strings.Contains(tt.body, "hopnet") {
				t.Fatalf("%s script does not reference 'hopnet'", tt.name)
			}
		})
	}
}
