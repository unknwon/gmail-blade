package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/pkg/errors"
)

const (
	cloudflareAPIURL          = "https://api.cloudflare.com/client/v4"
	cloudflareKVHighestUIDKey = "highest_uid"
)

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
	UID      imap.UID  `json:"uid"`
}

type cloudflareKVErrorResponse struct {
	Errors []struct {
		Code int `json:"code"`
	} `json:"errors"`
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

func (c *cloudflareKVCache) highestUID(ctx context.Context) (imap.UID, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.valueURL(), nil)
	if err != nil {
		return 0, errors.Wrap(err, "create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, errors.Wrap(err, "send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return 0, errors.Wrap(err, "read not found response")
		}
		var errorResponse cloudflareKVErrorResponse
		if err := json.Unmarshal(body, &errorResponse); err != nil {
			return 0, errors.Wrap(err, "decode not found response")
		}
		for _, apiError := range errorResponse.Errors {
			if apiError.Code == 10009 {
				return 0, nil
			}
		}
		return 0, errors.Errorf("unexpected response status %s: %s", resp.Status, body)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, cloudflareKVResponseError(resp)
	}

	var value cloudflareKVCacheValue
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return 0, errors.Wrap(err, "decode value")
	}
	return value.UID, nil
}

func (c *cloudflareKVCache) put(ctx context.Context, uid imap.UID, title string) error {
	data, err := json.Marshal(cloudflareKVCacheValue{
		CachedAt: time.Now().UTC(),
		Title:    title,
		UID:      uid,
	})
	if err != nil {
		return errors.Wrap(err, "marshal value")
	}

	url := fmt.Sprintf(
		"%s?expiration_ttl=%d",
		c.valueURL(),
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

func (c *cloudflareKVCache) valueURL() string {
	return fmt.Sprintf(
		"%s/accounts/%s/storage/kv/namespaces/%s/values/%s",
		c.baseURL,
		c.accountID,
		c.namespaceID,
		cloudflareKVHighestUIDKey,
	)
}

func cloudflareKVResponseError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return errors.Wrapf(err, "read response with status %s", resp.Status)
	}
	return errors.Errorf("unexpected response status %s: %s", resp.Status, body)
}
