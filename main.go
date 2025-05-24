package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/slack-go/slack"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Message struct {
	Type      string          `json:"type"`
	Message   json.RawMessage `json:"message"`
	SessionID string          `json:"session_id"`
}

type AssistantMessage struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Role       string        `json:"role"`
	Model      string        `json:"model"`
	Content    []ContentItem `json:"content"`
	StopReason string        `json:"stop_reason"`
}

type ContentItem struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

func main() {
	pflag.String("slack-bot-token", "", "Slack bot token")
	pflag.String("slack-channel-id", "", "Slack channel ID")
	pflag.String("slack-thread-ts", "", "Slack thread timestamp")
	pflag.Parse()

	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatalf("Error binding command line flags: %v", err)
	}

	viper.SetEnvPrefix("SLACK")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	slackBotToken := viper.GetString("slack-bot-token")
	slackChannelID := viper.GetString("slack-channel-id")
	slackThreadTS := viper.GetString("slack-thread-ts")

	// Flag to determine if we're in debug mode (no Slack credentials)
	debugMode := false

	// Check if Slack credentials are available
	if slackBotToken == "" || slackChannelID == "" || slackThreadTS == "" {
		if slackBotToken != "" {
			log.Fatal("Slack bot token is set but channel ID and/or thread timestamp are missing")
		}
		debugMode = true
		log.Println("Slack credentials not found. Running in debug mode, output will be printed to stdout")
	}

	// Create Slack client if not in debug mode
	var api *slack.Client
	if !debugMode {
		api = slack.New(slackBotToken)
	}

	// Set up reader for stdin that doesn't buffer full lines
	reader := bufio.NewReader(os.Stdin)

	// Buffer to accumulate JSON data
	var jsonBuffer strings.Builder

	for {
		// Read a single byte
		b, err := reader.ReadByte()

		if err != nil {
			if err == io.EOF {
				// End of file, process any remaining data in buffer
				processBuffer(&jsonBuffer, api, slackChannelID, slackThreadTS, debugMode)
				break
			}
			log.Fatalf("Error reading from stdin: %v", err)
		}

		// Add byte to buffer
		jsonBuffer.WriteByte(b)

		// If we see a newline, try to process the buffer as a complete JSON object
		if b == '\n' {
			processBuffer(&jsonBuffer, api, slackChannelID, slackThreadTS, debugMode)
			jsonBuffer.Reset()
		}
	}
}

func processBuffer(jsonBuffer *strings.Builder, api *slack.Client, channelID, threadTS string, debugMode bool) {
	jsonStr := jsonBuffer.String()
	jsonStr = strings.TrimSpace(jsonStr)

	if jsonStr == "" {
		return
	}

	var msg Message
	if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
		log.Printf("Error parsing JSON: %v", err)
		return
	}

	// Only process assistant messages
	if msg.Type != "assistant" {
		return
	}

	var assistantMsg AssistantMessage
	if err := json.Unmarshal(msg.Message, &assistantMsg); err != nil {
		log.Printf("Error parsing assistant message: %v", err)
		return
	}

	// Filter for tool executions and text messages
	var outputs []string
	for _, content := range assistantMsg.Content {
		if content.Type == "tool_use" {
			toolInfo := fmt.Sprintf("🔧 Tool executed: %s", content.Name)
			outputs = append(outputs, toolInfo)
		} else if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
			outputs = append(outputs, strings.TrimSpace(content.Text))
		}
	}

	// If there are outputs, either post to Slack or print to stdout
	if len(outputs) > 0 {
		message := strings.Join(outputs, "\n")

		timestampedMessage := message

		if debugMode {
			// Debug mode: print to stdout
			fmt.Println("DEBUG OUTPUT:")
			fmt.Println("-------------")
			fmt.Println(timestampedMessage)
			fmt.Println("-------------")
		} else {
			// Normal mode: post to Slack
			_, _, err := api.PostMessage(
				channelID,
				slack.MsgOptionText(timestampedMessage, false),
				slack.MsgOptionTS(threadTS),
			)

			if err != nil {
				log.Printf("Error posting to Slack: %v", err)
			} else {
				log.Printf("Posted to Slack: %s", message)
			}
		}
	}
}
