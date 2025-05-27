package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

func createToolUseContextBlock(content ContentItem, showInput bool) *slack.ContextBlock {
	var text string

	if showInput {
		inputStr := formatToolInput(content.Input)
		text = fmt.Sprintf("🔧 *Tool*: %s\n*Input*: %s", content.Name, inputStr)
	} else {
		text = fmt.Sprintf("🔧 *Tool*: %s", content.Name)
	}

	textObj := slack.NewTextBlockObject(slack.MarkdownType, text, false, false)

	return slack.NewContextBlock("", textObj)
}

func formatToolInput(input json.RawMessage) string {
	if len(input) == 0 {
		return "N/A"
	}

	var inputMap map[string]interface{}
	err := json.Unmarshal(input, &inputMap)
	if err == nil {
		if cmd, ok := inputMap["command"]; ok {
			if cmdStr, ok := cmd.(string); ok {
				return fmt.Sprintf("`%s`", cmdStr)
			}
		}

		inputJSON, err := json.MarshalIndent(inputMap, "", "  ")
		if err == nil {
			return fmt.Sprintf("```%s```", string(inputJSON))
		}
	}

	var inputStr string
	err = json.Unmarshal(input, &inputStr)
	if err == nil {
		return fmt.Sprintf("`%s`", inputStr)
	}

	return fmt.Sprintf("`%s`", strings.TrimSpace(string(input)))
}
