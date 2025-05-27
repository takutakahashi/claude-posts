# claude-posts

A tool for processing Claude AI assistant messages and posting them to Slack.

## Features

- Processes streaming JSON messages from Claude AI sessions
- Filters and formats relevant content (text and tool executions)
- Posts updates to designated Slack channels or threads
- Supports debug mode for local development

## Usage

### Environment Variables

Set the following environment variables for Slack integration:

```
SLACK_BOT_TOKEN=your-slack-bot-token
SLACK_CHANNEL_ID=your-slack-channel-id
SLACK_THREAD_TS=your-slack-thread-timestamp
```

If these variables are not set, the application will run in debug mode and output to stdout.

### Running the Application

Since this application consists of multiple Go files, use one of the following methods to run it:

```bash
# Method 1: Run the entire package
go run .

# Method 2: Specify all Go files
go run main.go tool_use.go

# Method 3: Build and run the binary
go build -o claude-posts
./claude-posts
```

Example with input from a file:

```bash
cat data/claude.jsonl | go run .
```

## Input Format

The application expects newline-delimited JSON on stdin with the following structure:

```json
{
  "type": "assistant",
  "message": {
    "id": "msg_123",
    "type": "message",
    "role": "assistant",
    "model": "claude-3",
    "content": [
      {"type": "text", "text": "Hello world"},
      {"type": "tool_use", "name": "bash", "id": "tool1", "input": {"command": "ls -la"}}
    ],
    "stop_reason": "end_turn"
  },
  "session_id": "session_123"
}
```

## Output Format

- Text messages are posted as plain text
- Tool executions are posted as context blocks with the tool name and input field
