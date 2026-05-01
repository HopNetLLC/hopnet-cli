package main

import "testing"

func TestVersionDefault(t *testing.T) {
	if version == "" {
		t.Error("version should not be empty")
	}
	if commit == "" {
		t.Error("commit should not be empty")
	}
}
