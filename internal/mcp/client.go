// Package mcp is a stdio MCP server that lets an AI agent drive a running
// Testhooks through its REST API. It adds no behaviour of its own: every tool
// is one or two calls to the documented API.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// client calls the Testhooks REST API.
type client struct {
	base  string // e.g. http://localhost:8080, no trailing slash
	token string // sent as Authorization: Bearer <token> when set
	http  *http.Client
}

func newClient(base, token string) *client {
	return &client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		// No automatic gzip, so a replay carries only the headers it was captured with.
		// No Timeout here: do applies defaultTimeout unless the context has its own
		// deadline, which a long-polling call sets higher.
		http: &http.Client{Transport: &http.Transport{
			Proxy:              http.ProxyFromEnvironment,
			DisableCompression: true,
		}},
	}
}

// captureURL is the webhook URL for an endpoint slug.
func (c *client) captureURL(slug string) string {
	return c.base + "/h/" + url.PathEscape(slug)
}

// defaultTimeout bounds an API call whose context has no deadline of its own.
const defaultTimeout = 30 * time.Second

// do sends a JSON request to the API and decodes a JSON reply into out
// (which may be nil). Non-2xx replies become errors carrying the API's message.
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
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
	res, err := c.http.Do(req)
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
	return json.Unmarshal(data, out)
}
