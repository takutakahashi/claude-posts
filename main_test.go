package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestProcessBuffer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool // whether we expect output
	}{
		{
			name:     "empty buffer",
			input:    "",
			expected: false,
		},
		{
			name:     "non-assistant message",
			input:    `{"type": "user", "message": {}, "session_id": "test"}`,
			expected: false,
		},
		{
			name: "assistant message with text",
			input: `{
				"type": "assistant",
				"message": {
					"id": "test",
					"type": "message", 
					"role": "assistant",
					"model": "claude-3",
					"content": [{"type": "text", "text": "Hello world"}],
					"stop_reason": "end_turn"
				},
				"session_id": "test"
			}`,
			expected: true,
		},
		{
			name: "assistant message with tool use",
			input: `{
				"type": "assistant",
				"message": {
					"id": "test",
					"type": "message",
					"role": "assistant",
					"model": "claude-3",
					"content": [{"type": "tool_use", "name": "bash", "id": "tool1"}],
					"stop_reason": "tool_use"
				},
				"session_id": "test"
			}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			var buffer strings.Builder
			buffer.WriteString(tt.input)

			// Test in debug mode to avoid needing Slack credentials
			processBuffer(&buffer, nil, "", "", true)

			// This is a basic test - in a real scenario you'd capture output
			// For now, we're just testing that the function doesn't panic
		})
	}
}

func TestMessageParsing(t *testing.T) {
	jsonStr := `{
		"type": "assistant",
		"message": {
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-sonnet",
			"content": [
				{"type": "text", "text": "Hello"},
				{"type": "tool_use", "name": "bash", "id": "tool1"}
			],
			"stop_reason": "end_turn"
		},
		"session_id": "session_123"
	}`

	var msg Message
	err := json.Unmarshal([]byte(jsonStr), &msg)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	if msg.Type != "assistant" {
		t.Errorf("Expected type 'assistant', got '%s'", msg.Type)
	}

	if msg.SessionID != "session_123" {
		t.Errorf("Expected session_id 'session_123', got '%s'", msg.SessionID)
	}

	var assistantMsg AssistantMessage
	err = json.Unmarshal(msg.Message, &assistantMsg)
	if err != nil {
		t.Fatalf("Failed to parse assistant message: %v", err)
	}

	if assistantMsg.ID != "msg_123" {
		t.Errorf("Expected ID 'msg_123', got '%s'", assistantMsg.ID)
	}

	if len(assistantMsg.Content) != 2 {
		t.Errorf("Expected 2 content items, got %d", len(assistantMsg.Content))
	}

	if assistantMsg.Content[0].Type != "text" {
		t.Errorf("Expected first content type 'text', got '%s'", assistantMsg.Content[0].Type)
	}

	if assistantMsg.Content[1].Type != "tool_use" {
		t.Errorf("Expected second content type 'tool_use', got '%s'", assistantMsg.Content[1].Type)
	}
}

func TestContentItem(t *testing.T) {
	content := ContentItem{
		Type: "text",
		Text: "Hello world",
	}

	if content.Type != "text" {
		t.Errorf("Expected type 'text', got '%s'", content.Type)
	}

	if content.Text != "Hello world" {
		t.Errorf("Expected text 'Hello world', got '%s'", content.Text)
	}
}

func TestViperConfiguration(t *testing.T) {
	viper.Reset()

	viper.SetEnvPrefix("SLACK")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	os.Setenv("SLACK_BOT_TOKEN", "test-token")
	os.Setenv("SLACK_CHANNEL_ID", "test-channel")
	os.Setenv("SLACK_THREAD_TS", "test-thread")

	if token := viper.GetString("bot-token"); token != "test-token" {
		t.Errorf("Expected bot-token to be 'test-token', got '%s'", token)
	}
	if channel := viper.GetString("channel-id"); channel != "test-channel" {
		t.Errorf("Expected channel-id to be 'test-channel', got '%s'", channel)
	}
	if thread := viper.GetString("thread-ts"); thread != "test-thread" {
		t.Errorf("Expected thread-ts to be 'test-thread', got '%s'", thread)
	}

	os.Unsetenv("SLACK_BOT_TOKEN")
	os.Unsetenv("SLACK_CHANNEL_ID")
	os.Unsetenv("SLACK_THREAD_TS")
}
