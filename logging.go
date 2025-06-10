package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	colorReset  = "\033[0m"
	colorBlue   = "\033[34m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
)

func printBlue(format string, args ...interface{}) {
	fmt.Printf(colorBlue+format+colorReset, args...)
}

func printGreen(format string, args ...interface{}) {
	fmt.Printf(colorGreen+format+colorReset, args...)
}

func printRed(format string, args ...interface{}) {
	fmt.Printf(colorRed+format+colorReset, args...)
}

func printYellow(format string, args ...interface{}) {
	fmt.Printf(colorYellow+format+colorReset, args...)
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

func generateRandomFilename(prefix string) (string, error) {
	bytes := make([]byte, 8)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s_%x.txt", prefix, bytes)
	return filepath.Join(os.TempDir(), filename), nil
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
