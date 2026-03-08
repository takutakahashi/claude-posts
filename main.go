package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
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
		watchFile(filePath, api, slackChannelID, slackThreadTS, debugMode)
		return
	}

	// Otherwise, read from stdin (original behavior)
	processStdin(api, slackChannelID, slackThreadTS, debugMode)
}

func processStdin(api *slack.Client, channelID, threadTS string, debugMode bool) {
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
				processBuffer(&jsonBuffer, api, channelID, threadTS, debugMode)
				break
			}
			log.Fatalf("Error reading from stdin: %v", err)
		}

		// Add byte to buffer
		jsonBuffer.WriteByte(b)

		// If we see a newline, try to process the buffer as a complete JSON object
		if b == '\n' {
			processBuffer(&jsonBuffer, api, channelID, threadTS, debugMode)
			jsonBuffer.Reset()
		}
	}
}

func watchFile(filePath string, api *slack.Client, channelID, threadTS string, debugMode bool) {
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
	var lastPosition int64

	// Process initial file content
	lastPosition = processFileFromPosition(filePath, lastPosition, api, channelID, threadTS, debugMode)

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
				lastPosition = processFileFromPosition(filePath, lastPosition, api, channelID, threadTS, debugMode)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func processFileFromPosition(filePath string, startPosition int64, api *slack.Client, channelID, threadTS string, debugMode bool) int64 {
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
				processBuffer(&jsonBuffer, api, channelID, threadTS, debugMode)
				break
			}
			log.Printf("Error reading file: %v", err)
			break
		}

		currentPosition++
		jsonBuffer.WriteByte(b)

		// If we see a newline, process the buffer
		if b == '\n' {
			processBuffer(&jsonBuffer, api, channelID, threadTS, debugMode)
			jsonBuffer.Reset()
		}
	}

	return currentPosition
}

// convertMarkdownToSlack converts Markdown-formatted text to Slack mrkdwn format.
// It skips content inside fenced code blocks to preserve their formatting.
func convertMarkdownToSlack(text string) string {
	// Split on fenced code blocks so we don't mangle their contents.
	codeBlockRe := regexp.MustCompile("(?s)```.*?```")

	var result strings.Builder
	lastIndex := 0
	for _, loc := range codeBlockRe.FindAllStringIndex(text, -1) {
		result.WriteString(convertMarkdownSyntax(text[lastIndex:loc[0]]))
		result.WriteString(text[loc[0]:loc[1]])
		lastIndex = loc[1]
	}
	result.WriteString(convertMarkdownSyntax(text[lastIndex:]))
	return result.String()
}

// convertMarkdownSyntax applies mrkdwn conversions to a non-code-block segment.
func convertMarkdownSyntax(text string) string {
	// Tables first (multi-line structure)
	text = convertTables(text)

	// Headers: # Title -> *Title*
	// Use a placeholder (\x00) so the italic pass below doesn't re-convert them.
	headerRe := regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	text = headerRe.ReplaceAllString(text, "\x00${1}\x00")

	// Bold: **text** or __text__ -> *text*
	// Also use the placeholder for the same reason.
	boldRe := regexp.MustCompile(`\*\*(.+?)\*\*`)
	text = boldRe.ReplaceAllString(text, "\x00${1}\x00")
	boldUnderRe := regexp.MustCompile(`__(.+?)__`)
	text = boldUnderRe.ReplaceAllString(text, "\x00${1}\x00")

	// Italic: *text* -> _text_ (only remaining single-asterisk pairs)
	// Use ${1} to avoid Go regexp treating the trailing _ as part of the group name.
	italicRe := regexp.MustCompile(`\*([^*\n]+?)\*`)
	text = italicRe.ReplaceAllString(text, "_${1}_")

	// Restore bold/header placeholders as Slack bold markers.
	text = strings.ReplaceAll(text, "\x00", "*")

	// Strikethrough: ~~text~~ -> ~text~
	strikeRe := regexp.MustCompile(`~~(.+?)~~`)
	text = strikeRe.ReplaceAllString(text, "~$1~")

	// Links: [label](url) -> <url|label>
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	text = linkRe.ReplaceAllString(text, "<$2|$1>")

	return text
}

// convertTables wraps Markdown table blocks in fenced code blocks so they
// render as monospace text in Slack (which has no native table support).
func convertTables(text string) string {
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))
	inTable := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isTableRow := strings.HasPrefix(trimmed, "|")
		if isTableRow {
			if !inTable {
				result = append(result, "```")
				inTable = true
			}
			result = append(result, line)
		} else {
			if inTable {
				result = append(result, "```")
				inTable = false
			}
			result = append(result, line)
		}
	}
	if inTable {
		result = append(result, "```")
	}
	return strings.Join(result, "\n")
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

	// Filter for text messages only (tool outputs are excluded)
	var textOutputs []string

	for _, content := range assistantMsg.Content {
		if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
			textOutputs = append(textOutputs, strings.TrimSpace(content.Text))
		}
	}

	// If there are text outputs, either post to Slack or print to stdout
	if len(textOutputs) > 0 {
		textMessage := convertMarkdownToSlack(strings.Join(textOutputs, "\n"))

		if debugMode {
			// Debug mode: print to stdout
			fmt.Println("DEBUG OUTPUT:")
			fmt.Println("-------------")
			fmt.Println(textMessage)
			fmt.Println("-------------")
		} else {
			// Normal mode: post to Slack
			_, _, err := api.PostMessage(channelID,
				slack.MsgOptionTS(threadTS),
				slack.MsgOptionText(textMessage, false),
			)

			if err != nil {
				log.Printf("Error posting to Slack: %v", err)
			} else {
				log.Printf("Posted to Slack: text message")
			}
		}
	}
}
