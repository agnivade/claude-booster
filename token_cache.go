package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

type tokenCache struct {
	cache map[string][]byte // hash -> response body
	mutex sync.RWMutex
}

var globalTokenCache = &tokenCache{
	cache: make(map[string][]byte),
}

func (tc *tokenCache) get(hash string) ([]byte, bool) {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()
	response, exists := tc.cache[hash]
	return response, exists
}

func (tc *tokenCache) set(hash string, response []byte) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	tc.cache[hash] = response
}

func hashRequestBody(body []byte) string {
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

// Context key for storing cache hash
type contextKey string

const cacheHashKey contextKey = "cache_hash"

func addCacheHashToContext(ctx context.Context, hash string) context.Context {
	return context.WithValue(ctx, cacheHashKey, hash)
}

func getCacheHashFromContext(ctx context.Context) (string, bool) {
	hash, ok := ctx.Value(cacheHashKey).(string)
	return hash, ok
}