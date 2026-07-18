package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

func TestCloudflareKVCacheContains(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "cached",
			statusCode: http.StatusOK,
			want:       true,
		},
		{
			name:       "not cached",
			statusCode: http.StatusNotFound,
			want:       false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer token" {
					t.Errorf("Authorization header = %q, want %q", got, "Bearer token")
				}
				if got := r.URL.Path; got != "/accounts/account/storage/kv/namespaces/namespace/values/123" {
					t.Errorf("path = %q", got)
				}
				w.WriteHeader(test.statusCode)
			}))
			defer server.Close()

			cache := testCloudflareKVCache(server)
			got, err := cache.contains(context.Background(), imap.UID(123))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("contains() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestCloudflareKVCachePut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPut)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer token")
		}
		if got := r.URL.Query().Get("expiration_ttl"); got != "7776000" {
			t.Errorf("expiration_ttl = %q, want %q", got, "7776000")
		}

		var value cloudflareKVCacheValue
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		if value.Title != "Test title" {
			t.Errorf("title = %q, want %q", value.Title, "Test title")
		}
		if value.CachedAt.IsZero() {
			t.Error("cached_at is zero")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cache := testCloudflareKVCache(server)
	if err := cache.put(context.Background(), imap.UID(123), "Test title"); err != nil {
		t.Fatal(err)
	}
}

func TestParseConfigCloudflareKV(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "token")
	path := filepath.Join(t.TempDir(), "gmail-blade.yml")
	if err := os.WriteFile(path, []byte(`
credentials:
  username: test@example.com
  password: password
cache:
  cloudflare_kv:
    account_id: account
    namespace_id: namespace
    api_token: $CLOUDFLARE_API_TOKEN
`), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := parseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Cache.CloudflareKV.APIToken; got != "token" {
		t.Errorf("api_token = %q, want %q", got, "token")
	}
	if got := config.Cache.CloudflareKV.ttlDuration; got != 90*24*time.Hour {
		t.Errorf("ttl = %s, want %s", got, 90*24*time.Hour)
	}
}

func testCloudflareKVCache(server *httptest.Server) *cloudflareKVCache {
	return &cloudflareKVCache{
		accountID:   "account",
		namespaceID: "namespace",
		apiToken:    "token",
		ttl:         90 * 24 * time.Hour,
		baseURL:     server.URL,
		httpClient:  server.Client(),
	}
}
