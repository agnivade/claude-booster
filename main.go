package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"

	"github.com/anthropics/anthropic-sdk-go"
)

// todo:
// parse the request into struct --
// disable haiku generation. --
// cache count_tokens response. --
// strip unnecessary tools from prompts. --
// strip unnecessary words from prompts like - IMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context or otherwise consider it in your response unless it is highly relevant to your task. Most of the time, it is not relevant.\n</system-reminder>\n
// re-arrange prompts to have tools earlier for better caching.
// add Prometheus metrics
// - change temperature
// - compress tokens
// - RAG
// - claude usage statistics.

func main() {
	targetURL := flag.String("target", "", "Target URL to proxy to (required)")
	listenAddr := flag.String("addr", "localhost", "Listen address")
	listenPort := flag.String("port", "8080", "Listen port")
	flag.Parse()

	if *targetURL == "" {
		log.Fatal("Target URL is required. Use -target flag.")
	}

	target, err := url.Parse(*targetURL)
	if err != nil {
		log.Fatalf("Invalid target URL: %v", err)
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
		},
	}

	// Add logging middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)

		// Check if this is an Anthropic API request that needs special handling
		if r.Method == "POST" {
			switch r.URL.Path {
			case "/v1/messages":
				if handleMessage(r, w) {
					return // Response already written
				}
			case "/v1/messages/count_tokens":
				if handleTokenCount(r, w) {
					return // Response already written
				}
			}
		}

		// Capture response
		responseWriter := &responseLogger{ResponseWriter: w}
		proxy.ServeHTTP(responseWriter, r)
		logResponse(responseWriter, r)
	})

	listenAddress := *listenAddr + ":" + *listenPort
	log.Printf("Starting reverse proxy on %s, forwarding to %s", listenAddress, *targetURL)

	err = http.ListenAndServe(listenAddress, handler)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

type responseLogger struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (r *responseLogger) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseLogger) Write(body []byte) (int, error) {
	r.body.Write(body)
	return r.ResponseWriter.Write(body)
}

func handleMessage(r *http.Request, w http.ResponseWriter) bool {
	// Read the request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		printRed("Error reading request body: %v\n", err)
		return false
	}

	// Parse into MessageNewParams
	var params anthropic.BetaMessageNewParams
	err = json.Unmarshal(bodyBytes, &params)
	if err != nil {
		printRed("Error parsing MessageNewParams: %v\n", err)
		return false
	}

	// Check if we should suppress Haiku generation
	if suppressHaikuGeneration(&params, w) {
		return true
	}

	var bodyModified bool
	if params.Model == anthropic.ModelClaudeSonnet4_20250514 {
		// Log basic information about the parsed request
		printYellow("Successfully parsed Anthropic request:\n")
		printYellow("  Max Tokens: %d\n", params.MaxTokens)
		printYellow("  Messages count: %d\n", len(params.Messages))
		// printYellow("  Tools count: %d\n", len(params.Tools))
		printYellow("  Temperature: %f\n", params.Temperature.Value)
		// printYellow("  Thinking: %v, %v\n", params.Thinking.GetBudgetTokens(), params.Thinking.GetType())

		// Set temperature
		params.Temperature = anthropic.Float(0.1)
		bodyModified = true
	}

	// Process tools and update request if needed
	bodyModified = bodyModified || filterTools(&params)

	// Additional message handling logic can go here

	// Marshal and set body if any modifications were made
	if bodyModified {
		modifiedBody, err := json.Marshal(params)
		if err != nil {
			printRed("Error marshaling modified request: %v\n", err)
			return false
		}

		writeToFile(modifiedBody, "june_10_temperature")

		r.Body = io.NopCloser(bytes.NewReader(modifiedBody))
		r.ContentLength = int64(len(modifiedBody))
		r.Header.Set("Content-Length", strconv.Itoa(len(modifiedBody)))
	} else {
		// Restore original body if no modifications
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}


	return false
}

func filterTools(params *anthropic.BetaMessageNewParams) bool {
	if params.Model != anthropic.ModelClaudeSonnet4_20250514 || len(params.Tools) == 0 {
		return false
	}

	// Filter out NotebookRead and NotebookEdit tools
	originalLen := len(params.Tools)
	filtered := params.Tools[:0]
	for _, tool := range params.Tools {
		name := *tool.GetName()
		if name != "NotebookRead" && name != "NotebookEdit" {
			filtered = append(filtered, tool)
		}
	}

	// If tools were filtered, update params
	if len(filtered) != originalLen {
		printYellow("Filtered out %d/%d tools.\n", originalLen-len(filtered), originalLen)
		params.Tools = filtered
		return true
	}

	return false
}

func handleTokenCount(r *http.Request, w http.ResponseWriter) bool {
	// Read the request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		printRed("Error reading token count request body: %v\n", err)
		return false
	}

	// Restore the body for potential forwarding
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Generate hash for cache key
	hash := hashRequestBody(bodyBytes)

	// Check if we have a cached response
	if cachedResponse, exists := globalTokenCache.get(hash); exists {
		printGreen("Token count cache hit! Returning cached response\n")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(cachedResponse)))
		w.WriteHeader(http.StatusOK)
		w.Write(cachedResponse)
		return true
	}

	// Cache miss - add hash to context for response caching
	printYellow("Token count cache miss. Request will be forwarded and response cached\n")
	ctx := addCacheHashToContext(r.Context(), hash)
	*r = *r.WithContext(ctx)

	return false
}
