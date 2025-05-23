FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o claude-posts .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/claude-posts .

# Create data directory
RUN mkdir -p /data

# Set environment variables
ENV SLACK_TOKEN=""
ENV SLACK_CHANNEL_ID=""
ENV SLACK_THREAD_TS=""

CMD ["./claude-posts"]