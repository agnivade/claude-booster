package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

func suppressHaikuGeneration(params *anthropic.MessageNewParams, w http.ResponseWriter) bool {
	// Check conditions for suppression
	if params.Model != anthropic.ModelClaude3_5Haiku20241022 {
		return false
	}

	// Check for exactly 1 system message with specific content
	if len(params.System) != 1 {
		return false
	}

	targetSystemMessage := "Analyze this message and come up with a single positive, cheerful and delightful verb in gerund form that's related to the message. Only include the word with no other text or punctuation. The word should have the first letter capitalized. Add some whimsy and surprise to entertain the user. Ensure the word is highly relevant to the user's message."
	if !strings.Contains(params.System[0].Text, targetSystemMessage) {
		return false
	}

	printYellow("Suppressing request - conditions met!\n")
	sendStreamingResponse(w)
	return true
}

func sendStreamingResponse(w http.ResponseWriter) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Generate random ID
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	messageID := fmt.Sprintf("msg_%x", randomBytes)

	// 1. Send message_start event
	messageStart := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         "claude-3-5-haiku-20241022",
			"content":       []string{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":                207,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     0,
				"output_tokens":               2,
				"service_tier":                "standard",
			},
		},
	}
	sendSSEEvent(w, "message_start", messageStart)
	flusher.Flush()

	// 2. Send content_block_start event
	contentBlockStart := map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	}
	sendSSEEvent(w, "content_block_start", contentBlockStart)
	flusher.Flush()

	// 3. Send ping event
	ping := map[string]any{
		"type": "ping",
	}
	sendSSEEvent(w, "ping", ping)
	flusher.Flush()

	// 4. Send content_block_delta event
	contentDelta := map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type": "text_delta",
			"text": "Processing",
		},
	}
	sendSSEEvent(w, "content_block_delta", contentDelta)
	flusher.Flush()

	// 5. Send content_block_stop event
	contentBlockStop := map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	}
	sendSSEEvent(w, "content_block_stop", contentBlockStop)
	flusher.Flush()

	// 6. Send message_delta event
	messageDelta := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": 5,
		},
	}
	sendSSEEvent(w, "message_delta", messageDelta)
	flusher.Flush()

	// 7. Send message_stop event
	messageStop := map[string]any{
		"type": "message_stop",
	}
	sendSSEEvent(w, "message_stop", messageStop)
	flusher.Flush()

	printGreen("Sent streaming response successfully\n")
}

func sendSSEEvent(w http.ResponseWriter, event string, data any) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
}
