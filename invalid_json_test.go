package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExitOnInvalidJSON(t *testing.T) {
	cmd := exec.Command("go", "build", "-o", "claude-posts-test")
	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to build test binary: %v", err)
	}
	defer os.Remove("claude-posts-test")

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "invalid main JSON",
			input: `{"type": "assistant", "message": {}, "session_id": "test"`,
		},
		{
			name:  "invalid assistant message JSON",
			input: `{"type": "assistant", "message": {"invalid": json}, "session_id": "test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("./claude-posts-test")
			cmd.Stdin = strings.NewReader(tt.input)

			err := cmd.Run()
			if err == nil {
				t.Errorf("Expected program to exit with error for invalid JSON, but it succeeded")
			}
		})
	}
}
