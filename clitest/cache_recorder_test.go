package clitest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
)

type cacheSave struct {
	Key   string
	Paths []string
	Name  string
}

type cacheRestore struct {
	Keys []string
	Name string
}

// cacheRecorder is a fake CircleCI plugin server that records save/restore
// calls made against it, for asserting how setup-python drives the cache SDK.
// It stands in for the sandbox's own cache when a test needs to inspect the
// exact keys, paths and step names, which the round-trip cache does not
// expose.
type cacheRecorder struct {
	mu       sync.Mutex
	restores []cacheRestore
	saves    []cacheSave
}

func newCacheRecorder(t *testing.T) (*cacheRecorder, string) {
	t.Helper()
	rec := &cacheRecorder{}

	mux := http.NewServeMux()
	mux.HandleFunc("/step/cache/restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Keys []string `json:"keys"`
			Name string   `json:"name"`
		}
		assert.NilError(t, json.NewDecoder(r.Body).Decode(&req))
		rec.mu.Lock()
		rec.restores = append(rec.restores, cacheRestore{Keys: req.Keys, Name: req.Name})
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "success"})
	})
	mux.HandleFunc("/step/cache/save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key   string   `json:"key"`
			Paths []string `json:"paths"`
			Name  string   `json:"name"`
		}
		assert.NilError(t, json.NewDecoder(r.Body).Decode(&req))
		rec.mu.Lock()
		rec.saves = append(rec.saves, cacheSave{Key: req.Key, Paths: req.Paths, Name: req.Name})
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "success"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return rec, srv.URL
}
