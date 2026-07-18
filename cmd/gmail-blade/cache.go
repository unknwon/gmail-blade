package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/pkg/errors"
)

const cloudflareAPIURL = "https://api.cloudflare.com/client/v4"

type cloudflareKVCache struct {
	accountID   string
	namespaceID string
	apiToken    string
	ttl         time.Duration
	baseURL     string
	httpClient  *http.Client
}

type cloudflareKVCacheValue struct {
	CachedAt time.Time `json:"cached_at"`
	Title    string    `json:"title"`
}

func newCloudflareKVCache(config configCloudflareKV) *cloudflareKVCache {
	if !config.enabled() {
		return nil
	}
	return &cloudflareKVCache{
		accountID:   config.AccountID,
		namespaceID: config.NamespaceID,
		apiToken:    config.APIToken,
		ttl:         config.ttlDuration,
		baseURL:     cloudflareAPIURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *cloudflareKVCache) contains(ctx context.Context, uid imap.UID) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.valueURL(uid), nil)
	if err != nil {
		return false, errors.Wrap(err, "create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, errors.Wrap(err, "send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return false, cloudflareKVResponseError(resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return true, nil
}

func (c *cloudflareKVCache) put(ctx context.Context, uid imap.UID, title string) error {
	data, err := json.Marshal(cloudflareKVCacheValue{
		CachedAt: time.Now().UTC(),
		Title:    title,
	})
	if err != nil {
		return errors.Wrap(err, "marshal value")
	}

	url := fmt.Sprintf(
		"%s?expiration_ttl=%d",
		c.valueURL(uid),
		int64(c.ttl/time.Second),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return errors.Wrap(err, "create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return cloudflareKVResponseError(resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *cloudflareKVCache) valueURL(uid imap.UID) string {
	return fmt.Sprintf(
		"%s/accounts/%s/storage/kv/namespaces/%s/values/%s",
		c.baseURL,
		c.accountID,
		c.namespaceID,
		strconv.FormatUint(uint64(uid), 10),
	)
}

func cloudflareKVResponseError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return errors.Wrapf(err, "read response with status %s", resp.Status)
	}
	return errors.Errorf("unexpected response status %s: %s", resp.Status, body)
}
