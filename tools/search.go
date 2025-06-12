package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/vectorstores/pgvector"

	_ "github.com/lib/pq"
)

func main() {
	// Parse command line flags
	query := flag.String("query", "", "Search query")
	modelName := flag.String("model", "llama2", "Ollama model name for embeddings")
	limit := flag.Int("limit", 3, "Number of results to return")
	flag.Parse()

	if *query == "" {
		log.Fatal("Please provide a search query using -query flag")
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

	// Perform similarity search
	fmt.Printf("Searching for: %s\n", *query)
	results, err := store.SimilaritySearch(ctx, *query, *limit)
	if err != nil {
		log.Fatalf("Failed to perform similarity search: %v", err)
	}

	fmt.Printf("\nFound %d results:\n", len(results))
	for i, result := range results {
		fmt.Printf("\n--- Result %d ---\n", i+1)
		fmt.Printf("Content: %s\n", truncateString(result.PageContent, 200))
		fmt.Printf("Metadata: %v\n", result.Metadata)
		fmt.Printf("Score: %f\n", result.Score)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
	}
}

func truncateString(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length] + "..."
}