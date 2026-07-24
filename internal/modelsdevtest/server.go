// Package modelsdevtest provides shared models.dev test doubles: fixture
// providers and the httptest servers that serve them, so the root-package library
// tests and the CLI end-to-end tests exercise the same deterministic, network-free
// models.dev rather than each copying its own.
package modelsdevtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

// CountingServer is Server with a fetch counter, so a test can assert that a
// single operation fetches models.dev once however many goroutines it fans out.
func CountingServer(t *testing.T, present []string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	data := catalogJSON(present, nil)
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// Server serves a tailored models.dev catalog.json. present lists the providers to
// expose (each with one valid model); malformed lists providers exposed with a
// model that fails validation, to trigger ErrModelsSchema.
func Server(t *testing.T, present []string, malformed ...string) *httptest.Server {
	t.Helper()
	data := catalogJSON(present, malformed)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Closed returns the URL of a server that is already shut down, so a fetch against
// it fails: the no-network, no-cache condition.
func Closed(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}

// MustNotFetch returns a models.dev URL whose any access fails the test: proof
// that a code path is answered without touching models.dev.
func MustNotFetch(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("models.dev was fetched; this path must stay offline")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// ReleaseDates gives each fixture provider's model a distinct release date, so
// newest-first ordering is observable across providers: openai's model is newer
// than google's even though google sorts first by id.
var ReleaseDates = map[string]string{
	"anthropic": "2025-06-01",
	"google":    "2024-01-01",
	"openai":    "2025-01-01",
}

// Provider builds one fixture provider with a single model. anthropic carries the
// "claude-sonnet" model so the canonical-id path resolves; other providers carry a
// generic model. A malformed provider's model has an empty id, which fails the
// per-model check.
func Provider(pid string, malformed bool) modelsdev.Provider {
	key, name := pid+"-model", pid+" model"
	if pid == "anthropic" {
		key, name = "claude-sonnet", "Claude Sonnet"
	}
	id := key
	if malformed {
		id = ""
	}
	return modelsdev.Provider{
		ID:   pid,
		Name: strings.ToUpper(pid[:1]) + pid[1:],
		Env:  []string{strings.ToUpper(pid) + "_API_KEY"},
		Models: map[string]modelsdev.Model{
			key: {
				ID:          id,
				Name:        name,
				ReleaseDate: ReleaseDates[pid],
				Limit:       modelsdev.Limit{Context: 200000, Output: 8192},
				Cost:        &modelsdev.Cost{Input: 3, Output: 15},
			},
		},
	}
}

// catalogJSON marshals a models.dev catalog.json carrying the requested providers.
// The agnostic map carries one canonical id so the top-level shape validates and
// the canonical-id path resolves.
func catalogJSON(present, malformed []string) []byte {
	cat := modelsdev.Catalog{
		Models: map[string]modelsdev.Model{
			"anthropic/claude-sonnet": {ID: "anthropic/claude-sonnet", Name: "Claude Sonnet", Limit: modelsdev.Limit{Context: 200000}},
		},
		Providers: map[string]modelsdev.Provider{},
	}
	for _, pid := range present {
		cat.Providers[pid] = Provider(pid, false)
	}
	for _, pid := range malformed {
		cat.Providers[pid] = Provider(pid, true)
	}
	data, err := json.Marshal(cat)
	if err != nil {
		panic(err)
	}
	return data
}
