package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
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
	pflag.Bool("show-input", true, "Show input field in tool execution messages")
	pflag.String("file", "", "JSONL file to watch for changes")
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
	showInput := viper.GetBool("show-input")
	filePath := viper.GetString("file")

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

	// If file path is provided, watch the file for changes
	if filePath != "" {
		watchFile(filePath, api, slackChannelID, slackThreadTS, debugMode, showInput)
		return
	}

	// Otherwise, read from stdin (original behavior)
	processStdin(api, slackChannelID, slackThreadTS, debugMode, showInput)
}

func processStdin(api *slack.Client, channelID, threadTS string, debugMode bool, showInput bool) {
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
				processBuffer(&jsonBuffer, api, channelID, threadTS, debugMode, showInput)
				break
			}
			log.Fatalf("Error reading from stdin: %v", err)
		}

		// Add byte to buffer
		jsonBuffer.WriteByte(b)

		// If we see a newline, try to process the buffer as a complete JSON object
		if b == '\n' {
			processBuffer(&jsonBuffer, api, channelID, threadTS, debugMode, showInput)
			jsonBuffer.Reset()
		}
	}
}

func watchFile(filePath string, api *slack.Client, channelID, threadTS string, debugMode bool, showInput bool) {
	// Create new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Error creating file watcher: %v", err)
	}
	defer watcher.Close()

	// Add file to watcher
	err = watcher.Add(filePath)
	if err != nil {
		log.Fatalf("Error watching file %s: %v", filePath, err)
	}

	log.Printf("Watching file: %s", filePath)

	// Track file position to only read new content
	var lastPosition int64 = 0

	// Process initial file content
	lastPosition = processFileFromPosition(filePath, lastPosition, api, channelID, threadTS, debugMode, showInput)

	// Watch for file changes
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Process on write events
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Printf("File modified: %s", event.Name)
				// Small delay to ensure write is complete
				time.Sleep(100 * time.Millisecond)
				lastPosition = processFileFromPosition(filePath, lastPosition, api, channelID, threadTS, debugMode, showInput)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func processFileFromPosition(filePath string, startPosition int64, api *slack.Client, channelID, threadTS string, debugMode bool, showInput bool) int64 {
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Error opening file: %v", err)
		return startPosition
	}
	defer file.Close()

	// Seek to the last read position
	_, err = file.Seek(startPosition, 0)
	if err != nil {
		log.Printf("Error seeking file: %v", err)
		return startPosition
	}

	reader := bufio.NewReader(file)
	var jsonBuffer strings.Builder
	currentPosition := startPosition

	for {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				// Process any remaining data
				processBuffer(&jsonBuffer, api, channelID, threadTS, debugMode, showInput)
				break
			}
			log.Printf("Error reading file: %v", err)
			break
		}

		currentPosition++
		jsonBuffer.WriteByte(b)

		// If we see a newline, process the buffer
		if b == '\n' {
			processBuffer(&jsonBuffer, api, channelID, threadTS, debugMode, showInput)
			jsonBuffer.Reset()
		}
	}

	return currentPosition
}

func processBuffer(jsonBuffer *strings.Builder, api *slack.Client, channelID, threadTS string, debugMode bool, showInput bool) {
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

	// Filter for tool executions and text messages
	var textOutputs []string
	var blocks []slack.Block

	for _, content := range assistantMsg.Content {
		if content.Type == "tool_use" {
			// Create context block for tool execution
			contextBlock := createToolUseContextBlock(content, showInput)
			blocks = append(blocks, contextBlock)
		} else if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
			textOutputs = append(textOutputs, strings.TrimSpace(content.Text))
		}
	}

	// If there are outputs, either post to Slack or print to stdout
	if len(textOutputs) > 0 || len(blocks) > 0 {
		textMessage := ""
		if len(textOutputs) > 0 {
			textMessage = strings.Join(textOutputs, "\n")
		}

		if debugMode {
			// Debug mode: print to stdout
			fmt.Println("DEBUG OUTPUT:")
			fmt.Println("-------------")
			if textMessage != "" {
				fmt.Println(textMessage)
			}

			// Print block information in debug mode
			for _, block := range blocks {
				if contextBlock, ok := block.(*slack.ContextBlock); ok {
					for _, elem := range contextBlock.ContextElements.Elements {
						if textObj, ok := elem.(*slack.TextBlockObject); ok {
							fmt.Println(textObj.Text)
						}
					}
				}
			}
			fmt.Println("-------------")
		} else {
			// Normal mode: post to Slack
			var options []slack.MsgOption

			options = append(options, slack.MsgOptionTS(threadTS))

			// Add text if available
			if textMessage != "" {
				options = append(options, slack.MsgOptionText(textMessage, false))
			}

			// Add blocks if available
			if len(blocks) > 0 {
				options = append(options, slack.MsgOptionBlocks(blocks...))
			}

			_, _, err := api.PostMessage(channelID, options...)

			if err != nil {
				log.Printf("Error posting to Slack: %v", err)
			} else {
				log.Printf("Posted to Slack: text and/or blocks")
			}
		}
	}
}
