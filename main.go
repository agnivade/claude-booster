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
)

// todo:
// parse the request into struct
// disable haiku generation.
// cache count_tokens response.
// strip unnecessary words from prompts like - IMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context or otherwise consider it in your response unless it is highly relevant to your task. Most of the time, it is not relevant.\n</system-reminder>\n
// strip unnecessary tools from prompts.
// re-arrange prompts to have tools earlier for better caching.
// add Prometheus metrics

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

		// Capture response
		responseWriter := &responseLogger{ResponseWriter: w}

		proxy.ServeHTTP(responseWriter, r)

		logResponse(responseWriter)
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
	printBlue("=== REQUEST ===\n")
	printBlue("%s %s %s\n", r.Method, r.RequestURI, r.Proto)
	printBlue("Host: %s\n", r.Host)

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
	filename, err := generateRandomFilename("request")
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

func logResponse(w *responseLogger) {
	printGreen("=== RESPONSE ===\n")
	printGreen("Status: %d %s\n", w.statusCode, http.StatusText(w.statusCode))

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
