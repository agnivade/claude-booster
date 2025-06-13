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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/anthropics/anthropic-sdk-go"
)

type Config struct {
	SuppressHaiku bool
	Temperature   float64
	RootDir       string
}

type TemplateData struct {
	UserPrivate    string
	ProjectPrivate string
	ProjectPublic  string
}

func main() {
	targetURL := flag.String("target", "", "Target URL to proxy to (required)")
	listenAddr := flag.String("addr", "localhost", "Listen address")
	listenPort := flag.String("port", "8080", "Listen port")
	suppressHaiku := flag.Bool("suppress-haiku", false, "Enable haiku generation suppression")
	temperature := flag.Float64("temperature", 0.1, "Temperature for Claude Sonnet 4 requests")
	rootDir := flag.String("root-dir", "", "Root directory for project files (required)")
	flag.Parse()

	config := Config{
		SuppressHaiku: *suppressHaiku,
		Temperature:   *temperature,
		RootDir:       *rootDir,
	}

	if *targetURL == "" {
		log.Fatal("Target URL is required. Use -target flag.")
	}
	if *rootDir == "" {
		log.Fatal("Root directory is required. Use -root-dir flag.")
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
				if handleMessage(r, w, config) {
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

func handleMessage(r *http.Request, w http.ResponseWriter, config Config) bool {
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
	if config.SuppressHaiku && suppressHaikuGeneration(&params, w) {
		return true
	}

	if params.Model == anthropic.ModelClaudeSonnet4_20250514 {
		// Log basic information about the parsed request
		// printYellow("Successfully parsed Anthropic request:\n")
		// printYellow("  Max Tokens: %d\n", params.MaxTokens)
		// printYellow("  Messages count: %d\n", len(params.Messages))
		// printYellow("  Tools count: %d\n", len(params.Tools))
	}

	// Process modifications
	bodyModified := setTemperature(&params, config)
	bodyModified = setUserPrompt(&params, config) || bodyModified
	bodyModified = setSystemPrompt(&params) || bodyModified
	bodyModified = filterTools(&params) || bodyModified
	// This should be the last.
	bodyModified = alterCacheControl(&params) || bodyModified

	// Marshal and set body if any modifications were made
	if bodyModified {
		modifiedBody, err := json.Marshal(params)
		if err != nil {
			printRed("Error marshaling modified request: %v\n", err)
			return false
		}

		// writeToFile(modifiedBody, "june13")

		r.Body = io.NopCloser(bytes.NewReader(modifiedBody))
		r.ContentLength = int64(len(modifiedBody))
		r.Header.Set("Content-Length", strconv.Itoa(len(modifiedBody)))
	} else {
		// Restore original body if no modifications
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	return false
}

func setTemperature(params *anthropic.BetaMessageNewParams, config Config) bool {
	if params.Model != anthropic.ModelClaudeSonnet4_20250514 {
		return false
	}

	// We can't override temperature when thinking is enabled.
	if thinking := params.Thinking.OfEnabled; thinking != nil && thinking.Type == "enabled" {
		return false
	}

	params.Temperature = anthropic.Float(config.Temperature)
	return true
}

func setUserPrompt(params *anthropic.BetaMessageNewParams, config Config) bool {
	if params.Model != anthropic.ModelClaudeSonnet4_20250514 {
		return false
	}

	if len(params.Messages[0].Content) > 0 {
		if ptr := params.Messages[0].Content[0].GetText(); ptr != nil {
			txt := *ptr

			if strings.HasPrefix(txt, "<system-reminder>") {
				// Load template data
				templateData := loadTemplateData(config.RootDir)

				// Process template
				processedText, err := processTemplate("assets/user_prompt.txt", templateData)
				if err != nil {
					printRed("Error processing user_prompt.txt template: %v\n", err)
					return false
				}
				*ptr = processedText
				return true
			}
		}
	}

	return false
}

func setSystemPrompt(params *anthropic.BetaMessageNewParams) bool {
	if params.Model != anthropic.ModelClaudeSonnet4_20250514 {
		return false
	}

	if len(params.System) > 0 {
		// Read system prompt from file
		systemPromptText, err := os.ReadFile("assets/system_prompt.txt")
		if err != nil {
			printRed("Error reading system_prompt.txt: %v\n", err)
			return false
		}

		// Replace system prompt.
		params.System = []anthropic.BetaTextBlockParam{
			{
				// This must be there. Otherwise, it rejects the request.
				Text: "You are Claude Code, Anthropic's official CLI for Claude.",
				// CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
			},
			{
				Text:         string(systemPromptText),
				CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
			},
		}
		return true
	}

	return false
}

func alterCacheControl(params *anthropic.BetaMessageNewParams) bool {
	if params.Model != anthropic.ModelClaudeSonnet4_20250514 || len(params.Tools) == 0 {
		return false
	}

	// Set cache control on the last tool to ephemeral
	lastIndex := len(params.Tools) - 1
	lastTool := params.Tools[lastIndex]

	cacheControl := anthropic.NewBetaCacheControlEphemeralParam()

	// Check all possible tool types and set cache control
	// TODO: need to find a better way to achieve this.
	if lastTool.OfTool != nil {
		lastTool.OfTool.CacheControl = cacheControl
		return true
	} else if lastTool.OfComputerUseTool20241022 != nil {
		lastTool.OfComputerUseTool20241022.CacheControl = cacheControl
		return true
	} else if lastTool.OfBashTool20241022 != nil {
		lastTool.OfBashTool20241022.CacheControl = cacheControl
		return true
	} else if lastTool.OfTextEditor20241022 != nil {
		lastTool.OfTextEditor20241022.CacheControl = cacheControl
		return true
	} else if lastTool.OfComputerUseTool20250124 != nil {
		lastTool.OfComputerUseTool20250124.CacheControl = cacheControl
		return true
	} else if lastTool.OfBashTool20250124 != nil {
		lastTool.OfBashTool20250124.CacheControl = cacheControl
		return true
	} else if lastTool.OfTextEditor20250124 != nil {
		lastTool.OfTextEditor20250124.CacheControl = cacheControl
		return true
	} else if lastTool.OfTextEditor20250429 != nil {
		lastTool.OfTextEditor20250429.CacheControl = cacheControl
		return true
	} else if lastTool.OfWebSearchTool20250305 != nil {
		lastTool.OfWebSearchTool20250305.CacheControl = cacheControl
		return true
	} else if lastTool.OfCodeExecutionTool20250522 != nil {
		lastTool.OfCodeExecutionTool20250522.CacheControl = cacheControl
		return true
	}

	return false
}

func filterTools(params *anthropic.BetaMessageNewParams) bool {
	if params.Model != anthropic.ModelClaudeSonnet4_20250514 || len(params.Tools) == 0 {
		return false
	}

	var toolsModified bool
	// Replace long tool descriptions with shorter ones
	for _, tool := range params.Tools {
		name := *tool.GetName()
		if desc := tool.GetDescription(); desc != nil && len(*desc) > 5000 {
			printYellow("Tool '%s' has long description (%d chars), replacing...\n", name, len(*desc))

			var filename string
			switch name {
			case "Bash":
				filename = "assets/tool_bash_description.txt"
			case "TodoWrite":
				filename = "assets/tool_todowrite_description.txt"
			default:
				continue
			}

			newDesc, err := os.ReadFile(filename)
			if err != nil {
				printRed("Error reading %s: %v\n", filename, err)
				continue
			}

			// GetDescription != nil, means OfTool is not nil
			tool.OfTool.Description = anthropic.String(string(newDesc))

			toolsModified = true
		}
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
		toolsModified = true
	}

	return toolsModified
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

func loadFileContent(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			// Other errors (permissions, etc.) should be logged
			printYellow("Warning: Could not read file %s: %v\n", filePath, err)
		}
		return ""
	}
	return string(content)
}

func loadUserPrivate() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		printYellow("Warning: Could not get user home directory: %v\n", err)
		return ""
	}
	return loadFileContent(filepath.Join(homeDir, ".claude", "CLAUDE.md"))
}

func loadProjectPrivate(rootDir string) string {
	return loadFileContent(filepath.Join(rootDir, "CLAUDE.local.md"))
}

func loadProjectPublic(rootDir string) string {
	return loadFileContent(filepath.Join(rootDir, "CLAUDE.md"))
}

func loadTemplateData(rootDir string) TemplateData {
	return TemplateData{
		UserPrivate:    loadUserPrivate(),
		ProjectPrivate: loadProjectPrivate(rootDir),
		ProjectPublic:  loadProjectPublic(rootDir),
	}
}

func processTemplate(templatePath string, data TemplateData) (string, error) {
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("userPrompt").Parse(string(templateContent))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
