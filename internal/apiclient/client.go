// Package apiclient drives a running Testhooks through its REST API, for the
// MCP server and the CLI. It adds no behaviour of its own: every operation is
// one or two calls to the documented API.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client calls the Testhooks REST API. The MCP server and the CLI are both
// built on it, so they offer the same operations with the same words.
type Client struct {
	base  string // e.g. http://localhost:8080, no trailing slash
	token string // sent as Authorization: Bearer <token> when set
	HTTP  *http.Client
}

func New(base, token string) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		// No automatic gzip, so a replay carries only the headers it was captured with.
		// No Timeout here: do applies defaultTimeout unless the context has its own
		// deadline, which a long-polling call sets higher.
		HTTP: &http.Client{Transport: &http.Transport{
			Proxy:              http.ProxyFromEnvironment,
			DisableCompression: true,
		}},
	}
}

// CaptureURL is the webhook URL for an endpoint slug.
func (c *Client) CaptureURL(slug string) string {
	return c.base + "/h/" + url.PathEscape(slug)
}

// DefaultURL is used when TESTHOOKS_URL is not set.
const DefaultURL = "http://localhost:8080"

// FromEnv returns a client for the Testhooks named by TESTHOOKS_URL (default
// DefaultURL), with TESTHOOKS_AUTH_TOKEN as the bearer token when set.
func FromEnv() *Client {
	base := os.Getenv("TESTHOOKS_URL")
	if base == "" {
		base = DefaultURL
	}
	return New(base, os.Getenv("TESTHOOKS_AUTH_TOKEN"))
}

// defaultTimeout bounds an API call whose context has no deadline of its own.
const defaultTimeout = 30 * time.Second

// Do sends a JSON request to the API and decodes a JSON reply into out
// (which may be nil). Non-2xx replies become errors carrying the API's message.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Testhooks at %s (is it running? set TESTHOOKS_URL): %w", c.base, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var apiErr struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(data))
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			msg = apiErr.Error
		}
		if res.StatusCode == http.StatusUnauthorized {
			msg += " (set TESTHOOKS_AUTH_TOKEN to the server's AUTH_TOKEN)"
		}
		return fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, msg)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		// A route this server doesn't have falls through to the UI, which
		// answers 200 with HTML. Say so instead of quoting a parse error.
		if !strings.Contains(res.Header.Get("Content-Type"), "json") {
			return fmt.Errorf("%s %s: this Testhooks answered with %s, not JSON; it is probably older than this client and lacks this operation. Upgrade it, or use a newer one", method, path, firstWord(res.Header.Get("Content-Type"), "something"))
		}
		return fmt.Errorf("%s %s: unexpected reply: %w", method, path, err)
	}
	return nil
}

// firstWord returns the media type part of a Content-Type, or def when empty.
func firstWord(ct, def string) string {
	ct, _, _ = strings.Cut(ct, ";")
	if ct = strings.TrimSpace(ct); ct != "" {
		return ct
	}
	return def
}
