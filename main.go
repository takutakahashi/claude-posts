package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"bytes"

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
	pflag.String("bot-token", "", "Slack bot token")
	pflag.String("channel-id", "", "Slack channel ID")
	pflag.String("thread-ts", "", "Slack thread timestamp")
	pflag.Parse()

	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatalf("Error binding command line flags: %v", err)
	}

	viper.SetEnvPrefix("SLACK")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	slackBotToken := viper.GetString("bot-token")
	slackChannelID := viper.GetString("channel-id")
	slackThreadTS := viper.GetString("thread-ts")

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

func createToolExecutionBlocks(toolName string, input json.RawMessage) []slack.Block {
	// Create header section with subtle formatting
	headerText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*Tool executed:* %s", toolName), false, false)
	headerSection := slack.NewSectionBlock(headerText, nil, nil)

	// Create blocks for the message
	blocks := []slack.Block{headerSection}

	if len(input) > 0 {
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, input, "", "  "); err == nil {
			// Create input section with code formatting
			codeText := prettyJSON.String()
			if codeText == "null" || codeText == "" {
				codeText = "{}"
			}
			
			inputText := slack.NewTextBlockObject(
				"mrkdwn",
				fmt.Sprintf("*Input:*\n```%s```", codeText),
				false,
				false,
			)
			
			inputSection := slack.NewSectionBlock(inputText, nil, nil)
			blocks = append(blocks, inputSection)
		}
	}

	return blocks
}

func processBuffer(jsonBuffer *strings.Builder, api *slack.Client, channelID, threadTS string, debugMode bool) {
	jsonStr := jsonBuffer.String()
	jsonStr = strings.TrimSpace(jsonStr)

	if jsonStr == "" {
		return
	}

	var msg Message
	if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
		log.Fatalf("Error parsing JSON: %v", err)
	}

	// Only process assistant messages
	if msg.Type != "assistant" {
		return
	}

	var assistantMsg AssistantMessage
	if err := json.Unmarshal(msg.Message, &assistantMsg); err != nil {
		log.Fatalf("Error parsing assistant message: %v", err)
	}

	// Process tool executions and text messages
	var textOutputs []string
	var blocks []slack.Block

	for _, content := range assistantMsg.Content {
		if content.Type == "tool_use" {
			// Create Block Kit message for tool execution
			toolBlocks := createToolExecutionBlocks(content.Name, content.Input)
			blocks = append(blocks, toolBlocks...)
			
			if debugMode {
				var inputStr string
				if len(content.Input) > 0 {
					var prettyJSON bytes.Buffer
					if err := json.Indent(&prettyJSON, content.Input, "", "  "); err == nil {
						inputStr = fmt.Sprintf("\nInput:\n```\n%s\n```", prettyJSON.String())
					}
				}
				textOutputs = append(textOutputs, fmt.Sprintf("Tool executed: %s%s", content.Name, inputStr))
			}
		} else if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
			text := strings.TrimSpace(content.Text)
			textOutputs = append(textOutputs, text)
			
			textBlock := slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", text, false, false), nil, nil)
			blocks = append(blocks, textBlock)
		}
	}

	// If there are outputs, either post to Slack or print to stdout
	if len(blocks) > 0 || len(textOutputs) > 0 {
		if debugMode {
			// Debug mode: print to stdout
			fmt.Println("DEBUG OUTPUT:")
			fmt.Println("-------------")
			for _, text := range textOutputs {
				fmt.Println(text)
			}
			fmt.Println("-------------")
		} else {
			// Normal mode: post to Slack using Block Kit
			if len(blocks) > 0 {
				_, _, err := api.PostMessage(
					channelID,
					slack.MsgOptionBlocks(blocks...),
					slack.MsgOptionTS(threadTS),
				)

				if err != nil {
					log.Printf("Error posting to Slack: %v", err)
				} else {
					log.Printf("Posted to Slack with Block Kit formatting")
				}
			} else {
				message := strings.Join(textOutputs, "\n")
				_, _, err := api.PostMessage(
					channelID,
					slack.MsgOptionText(message, false),
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
}
