package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/schema"
	"github.com/tmc/langchaingo/vectorstores/pgvector"

	_ "github.com/lib/pq"
)

func main() {
	// Parse command line flags
	dirPath := flag.String("dir", "", "Directory containing documents to index")
	modelName := flag.String("model", "llama2", "Ollama model name for embeddings")
	extensions := flag.String("ext", "", "Comma-separated list of file extensions to process (e.g. .go,.md,.txt)")
	flag.Parse()

	if *dirPath == "" {
		log.Fatal("Please provide a directory path using -dir flag")
	}

	ctx := context.Background()

	// Initialize Ollama client
	ollamaLLM, err := ollama.New(ollama.WithModel(*modelName))
	if err != nil {
		log.Fatalf("Failed to create Ollama client: %v", err)
	}

	// Create embeddings client
	embedder, err := embeddings.NewEmbedder(ollamaLLM)
	if err != nil {
		log.Fatalf("Failed to create embeddings client: %v", err)
	}

	// Create pgvector store
	store, err := pgvector.New(
		ctx,
		pgvector.WithConnectionURL("postgres://mmuser:mostest@localhost/claude?sslmode=disable"),
		pgvector.WithEmbedder(embedder),
		pgvector.WithCollectionName("documents"),
	)
	if err != nil {
		log.Fatalf("Failed to create pgvector store: %v", err)
	}

	// Parse allowed extensions
	var allowedExts map[string]bool
	if *extensions != "" {
		allowedExts = make(map[string]bool)
		extList := strings.Split(*extensions, ",")
		for _, ext := range extList {
			ext = strings.TrimSpace(ext)
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			allowedExts[strings.ToLower(ext)] = true
		}
		fmt.Printf("Filtering for extensions: %v\n", extList)
	}

	// Process and index documents one by one
	fmt.Printf("Processing documents in directory: %s\n", *dirPath)
	count, err := processAndIndexDirectory(ctx, store, *dirPath, allowedExts)
	if err != nil {
		log.Fatalf("Failed to process directory: %v", err)
	}

	fmt.Printf("Successfully indexed %d documents\n", count)
}

func processAndIndexDirectory(ctx context.Context, store pgvector.Store, dirPath string, allowedExts map[string]bool) (int, error) {
	count := 0

	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Filter by allowed extensions if specified
		ext := strings.ToLower(filepath.Ext(path))
		if allowedExts != nil {
			if !allowedExts[ext] {
				return nil
			}
		} else {
			// Use default text file filtering if no extensions specified
			if !isTextFile(ext) {
				return nil
			}
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Failed to read file %s: %v", path, err)
			return nil
		}

		// Get file info
		fileInfo, err := d.Info()
		if err != nil {
			log.Printf("Failed to get file info for %s: %v", path, err)
			return nil
		}

		// Create metadata
		metadata := map[string]any{
			"file_path":     path,
			"file_name":     d.Name(),
			"file_size":     fileInfo.Size(),
			"modified_time": fileInfo.ModTime().Format("2006-01-02T15:04:05Z"),
			"file_type":     getFileType(ext),
		}

		// Create document
		doc := schema.Document{
			PageContent: string(content),
			Metadata:    metadata,
		}

		// Index this document immediately
		_, err = store.AddDocuments(ctx, []schema.Document{doc})
		if err != nil {
			log.Printf("Failed to index file %s: %v", path, err)
			return nil
		}

		count++
		fmt.Printf("Indexed: %s (%d bytes)\n", path, len(content))

		return nil
	})

	return count, err
}

func isTextFile(ext string) bool {
	textExtensions := map[string]bool{
		".txt":  true,
		".md":   true,
		".json": true,
		".yaml": true,
		".yml":  true,
		".toml": true,
		".go":   true,
		".py":   true,
		".js":   true,
		".ts":   true,
		".html": true,
		".css":  true,
		".xml":  true,
		".log":  true,
		".conf": true,
		".cfg":  true,
		"":      true, // files without extension
	}
	return textExtensions[ext]
}

func getFileType(ext string) string {
	switch ext {
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".xml":
		return "xml"
	case ".log":
		return "log"
	case ".conf", ".cfg":
		return "config"
	default:
		return "text"
	}
}
