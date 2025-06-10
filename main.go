package main

import (
	"bytes"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
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

func logRequest(r *http.Request) {
	printBlue("→ %s %s %s\n", r.Method, r.RequestURI, r.Proto)

	return
	// Log headers
	for name, values := range r.Header {
		for _, value := range values {
			printBlue("%s: %s\n", name, value)
		}
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		printRed("Error reading request body: %v\n", err)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	// printBlue("\nBody:\n%s\n", string(bodyBytes))

	// Write request body to file
	writeToFile(bodyBytes, "claude_june10")
}

func writeToFile(bodyBytes []byte, prefix string) {
	filename, err := generateRandomFilename(prefix)
	if err != nil {
		printRed("Error generating filename: %v\n", err)
		return
	}
	err = os.WriteFile(filename, bodyBytes, 0644)
	if err != nil {
		printRed("Error writing request to file: %v\n", err)
		return
	}
	printYellow("Request body saved to: %s\n", filename)
}

func logResponse(w *responseLogger, r *http.Request) {
	if w.statusCode == http.StatusOK {
		printGreen("← %d %s\n", w.statusCode, http.StatusText(w.statusCode))
	} else {
		printRed("← %d %s\n", w.statusCode, http.StatusText(w.statusCode))
	}

	// Check if this is a token count response that needs caching
	if hash, ok := getCacheHashFromContext(r.Context()); ok {
		if w.statusCode == http.StatusOK && w.body.Len() > 0 {
			globalTokenCache.set(hash, w.body.Bytes())
			printYellow("Cached token count response with hash: %s\n", hash[:8]+"...")
		}
	}

	return
	// Log response headers
	for name, values := range w.Header() {
		for _, value := range values {
			printGreen("%s: %s\n", name, value)
		}
	}

	// Log response body
	if w.body.Len() > 0 {
		printGreen("\nBody:\n%s\n", w.body.String())
	}
}

func generateRandomFilename(prefix string) (string, error) {
	bytes := make([]byte, 8)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s_%x.txt", prefix, bytes)
	return filepath.Join(os.TempDir(), filename), nil
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
	err = params.UnmarshalJSON(bodyBytes)
	if err != nil {
		printRed("Error parsing MessageNewParams: %v\n", err)
		return false
	}

	// Check if we should suppress Haiku generation
	if suppressHaikuGeneration(&params, w) {
		return true
	}

	// Log basic information about the parsed request
	// printYellow("Successfully parsed Anthropic request:\n")
	// printYellow("  Model: %s\n", params.Model)
	// printYellow("  Max Tokens: %d\n", params.MaxTokens)
	// printYellow("  Messages count: %d\n", len(params.Messages))
	// printYellow("  Tools count: %d\n", len(params.Tools))

	// Process tools and update request if needed
	bodyModified := filterTools(&params, r)

	// If tools weren't filtered, restore original body
	if !bodyModified {
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Additional message handling logic can go here

	return false
}

func filterTools(params *anthropic.BetaMessageNewParams, r *http.Request) bool {
	if params.Model != anthropic.ModelClaudeSonnet4_20250514 || len(params.Tools) == 0 {
		return false
	}

	// printBlue("Tools in request:\n")
	// for i, tool := range params.Tools {
	// 	printBlue("  [%d] %s\n", i, *tool.GetName())
	// }

	// Filter out NotebookRead and NotebookEdit tools
	originalLen := len(params.Tools)
	filtered := params.Tools[:0]
	for _, tool := range params.Tools {
		name := *tool.GetName()
		if name != "NotebookRead" && name != "NotebookEdit" {
			filtered = append(filtered, tool)
		}
	}

	// var filteredTools []anthropic.ToolUnionParam
	// for _, tool := range params.Tools {
	// 	if name != "NotebookRead" && name != "NotebookEdit" {
	// 		filteredTools = append(filteredTools, tool)
	// 	}
	// }
	// return false
	// If tools were filtered, update the request body
	if len(filtered) != originalLen {
		printYellow("Filtered out %d/%d tools. Remaining: %d\n", len(params.Tools)-len(filtered), len(params.Tools))
		params.Tools = filtered

		// Marshal the modified params back to JSON
		// TODO: change to json.Marshal
		modifiedBody, err := params.MarshalJSON()
		if err != nil {
			printRed("Error marshaling modified request: %v\n", err)
			return false
		}
		// printBlue("\nBody:\n%s\n", string(modifiedBody))
		// writeToFile(modifiedBody, "tools_debug")

		// Replace the request body
		r.Body = io.NopCloser(bytes.NewReader(modifiedBody))
		r.ContentLength = int64(len(modifiedBody))
		r.Header.Set("Content-Length", strconv.Itoa(len(modifiedBody)))
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
