package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Endpoint is an endpoint as the API returns it, plus the URL to send webhooks to.
type Endpoint struct {
	ID         string         `json:"id"`
	Slug       string         `json:"slug"`
	Name       string         `json:"name"`
	Mode       string         `json:"mode"`
	CreatedAt  string         `json:"created_at"`
	Config     map[string]any `json:"config"`
	CaptureURL string         `json:"capture_url" jsonschema:"the URL to give a webhook provider; anything sent here is captured"`
}

func (c *client) withURL(ep *Endpoint) {
	ep.CaptureURL = c.captureURL(ep.Slug)
	if ep.Config == nil {
		ep.Config = map[string]any{}
	}
}

// --- create_endpoint

type CreateEndpointInput struct {
	Name     string         `json:"name,omitempty" jsonschema:"a label for the endpoint, e.g. stripe test"`
	SlugName string         `json:"slug_name,omitempty" jsonschema:"a word to start the URL with, e.g. stripe gives /h/stripe-acid-fable-gully; lowercase letters, numbers and dashes, up to 20"`
	Mode     string         `json:"mode,omitempty" jsonschema:"server (default): Testhooks stores and processes requests; browser: an open Testhooks tab does"`
	Config   map[string]any `json:"config,omitempty" jsonschema:"endpoint settings such as forward_url; see the REST API reference. Leave out for an endpoint that only captures"`
}

func (c *client) createEndpoint(ctx context.Context, _ *mcp.CallToolRequest, in CreateEndpointInput) (*mcp.CallToolResult, Endpoint, error) {
	if err := checkMode(in.Mode); err != nil {
		return nil, Endpoint{}, err
	}
	var ep Endpoint
	if err := c.do(ctx, http.MethodPost, "/api/endpoints", in, &ep); err != nil {
		return nil, Endpoint{}, err
	}
	c.withURL(&ep)
	return nil, ep, nil
}

func checkMode(mode string) error {
	if mode != "" && mode != "server" && mode != "browser" {
		return fmt.Errorf("mode must be server or browser, not %q", mode)
	}
	return nil
}

// --- list_endpoints

type ListEndpointsInput struct {
	Limit  int `json:"limit,omitempty" jsonschema:"how many to return, 1 to 200 (default 50)"`
	Offset int `json:"offset,omitempty" jsonschema:"how many to skip, for paging"`
}

type ListEndpointsOutput struct {
	Endpoints []Endpoint `json:"endpoints"`
}

func (c *client) listEndpoints(ctx context.Context, _ *mcp.CallToolRequest, in ListEndpointsInput) (*mcp.CallToolResult, ListEndpointsOutput, error) {
	var out ListEndpointsOutput
	if err := c.do(ctx, http.MethodGet, "/api/endpoints"+page(in.Limit, in.Offset), nil, &out); err != nil {
		return nil, ListEndpointsOutput{}, err
	}
	if out.Endpoints == nil {
		out.Endpoints = []Endpoint{}
	}
	for i := range out.Endpoints {
		c.withURL(&out.Endpoints[i])
	}
	return nil, out, nil
}

func page(limit, offset int) string {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// --- get_endpoint

type GetEndpointInput struct {
	ID   string `json:"id,omitempty" jsonschema:"the endpoint id"`
	Slug string `json:"slug,omitempty" jsonschema:"the short code in the capture URL /h/<slug>; use instead of id"`
}

func (c *client) getEndpoint(ctx context.Context, _ *mcp.CallToolRequest, in GetEndpointInput) (*mcp.CallToolResult, Endpoint, error) {
	var path string
	switch {
	case in.ID != "":
		path = "/api/endpoints/" + url.PathEscape(in.ID)
	case in.Slug != "":
		path = "/api/slug/" + url.PathEscape(in.Slug)
	default:
		return nil, Endpoint{}, errors.New("give id or slug")
	}
	var ep Endpoint
	if err := c.do(ctx, http.MethodGet, path, nil, &ep); err != nil {
		return nil, Endpoint{}, err
	}
	c.withURL(&ep)
	return nil, ep, nil
}

// --- update_endpoint_config

type UpdateConfigInput struct {
	ID     string         `json:"id" jsonschema:"the endpoint id"`
	Config map[string]any `json:"config" jsonschema:"only the settings to change, e.g. {\"forward_url\":\"https://example.com/hook\"}; other settings are kept. Set a field to null to remove it"`
}

func (c *client) updateConfig(ctx context.Context, _ *mcp.CallToolRequest, in UpdateConfigInput) (*mcp.CallToolResult, Endpoint, error) {
	if in.ID == "" {
		return nil, Endpoint{}, errors.New("id is required")
	}
	path := "/api/endpoints/" + url.PathEscape(in.ID)
	var cur Endpoint
	if err := c.do(ctx, http.MethodGet, path, nil, &cur); err != nil {
		return nil, Endpoint{}, err
	}
	// The API replaces config as a whole, so merge the change into what is stored.
	merged := mergeConfig(cur.Config, in.Config)
	var ep Endpoint
	if err := c.do(ctx, http.MethodPatch, path, map[string]any{"config": merged}, &ep); err != nil {
		return nil, Endpoint{}, err
	}
	c.withURL(&ep)
	return nil, ep, nil
}

// mergeConfig returns base with patch applied: nested objects merge, a null
// removes the field, anything else replaces it. Neither input is modified.
func mergeConfig(base, patch map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(patch))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(out, k)
			continue
		}
		pm, pok := v.(map[string]any)
		bm, bok := out[k].(map[string]any)
		if pok && bok {
			out[k] = mergeConfig(bm, pm)
		} else {
			out[k] = v
		}
	}
	return out
}

// --- delete_endpoint

type DeleteEndpointInput struct {
	ID string `json:"id" jsonschema:"the endpoint id"`
}

type DeleteEndpointOutput struct {
	Deleted string `json:"deleted"`
}

func (c *client) deleteEndpoint(ctx context.Context, _ *mcp.CallToolRequest, in DeleteEndpointInput) (*mcp.CallToolResult, DeleteEndpointOutput, error) {
	if in.ID == "" {
		return nil, DeleteEndpointOutput{}, errors.New("id is required")
	}
	if err := c.do(ctx, http.MethodDelete, "/api/endpoints/"+url.PathEscape(in.ID), nil, nil); err != nil {
		return nil, DeleteEndpointOutput{}, err
	}
	return nil, DeleteEndpointOutput{Deleted: in.ID}, nil
}

// --- list_requests

type ListRequestsInput struct {
	EndpointID string `json:"endpoint_id" jsonschema:"the endpoint id"`
	Limit      int    `json:"limit,omitempty" jsonschema:"how many to return, newest first, 1 to 200 (default 50)"`
	Offset     int    `json:"offset,omitempty" jsonschema:"how many to skip, for paging"`
}

type ListRequestsOutput struct {
	Requests []map[string]any `json:"requests"`
	Total    int              `json:"total"`
}

func (c *client) listRequests(ctx context.Context, _ *mcp.CallToolRequest, in ListRequestsInput) (*mcp.CallToolResult, ListRequestsOutput, error) {
	if in.EndpointID == "" {
		return nil, ListRequestsOutput{}, errors.New("endpoint_id is required")
	}
	var out ListRequestsOutput
	path := "/api/endpoints/" + url.PathEscape(in.EndpointID) + "/requests" + page(in.Limit, in.Offset)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, ListRequestsOutput{}, err
	}
	if out.Requests == nil {
		out.Requests = []map[string]any{}
	}
	return nil, out, nil
}

// --- wait_for_request

// maxServerWait is the longest one ListRequests call may wait (the API caps
// wait at 60 seconds); longer timeouts are several calls.
const maxServerWait = 60 * time.Second

// pollInterval is used against a Testhooks too old to honour wait: it answers
// at once, so the tool asks again after this long.
const pollInterval = time.Second

type WaitForRequestInput struct {
	EndpointID     string `json:"endpoint_id" jsonschema:"the endpoint id"`
	After          string `json:"after,omitempty" jsonschema:"the id of the last request you have already seen, so only newer ones are returned. Leave out to take any request, including ones that arrived before this call"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"how long to wait, 1 to 300 (default 60). Call again with the same arguments to keep waiting"`
}

type WaitForRequestOutput struct {
	Requests []map[string]any `json:"requests" jsonschema:"the requests that arrived, newest first; empty when the wait timed out"`
	TimedOut bool             `json:"timed_out" jsonschema:"true when nothing arrived in time: the provider has not sent anything yet, so check the URL is configured there, or wait again"`
}

func (c *client) waitForRequest(ctx context.Context, _ *mcp.CallToolRequest, in WaitForRequestInput) (*mcp.CallToolResult, WaitForRequestOutput, error) {
	if in.EndpointID == "" {
		return nil, WaitForRequestOutput{}, errors.New("endpoint_id is required")
	}
	timeout := 60 * time.Second
	if in.TimeoutSeconds > 0 {
		timeout = min(time.Duration(in.TimeoutSeconds)*time.Second, 300*time.Second)
	}

	// The server filters by after; this fetch repeats the filter here for a
	// Testhooks too old to know the parameter, and gives a clear error when the
	// request is gone.
	var since cursor
	if in.After != "" {
		var prev struct {
			ID         string    `json:"id"`
			EndpointID string    `json:"endpoint_id"`
			CreatedAt  time.Time `json:"created_at"`
		}
		if err := c.do(ctx, http.MethodGet, "/api/requests/"+url.PathEscape(in.After), nil, &prev); err != nil {
			return nil, WaitForRequestOutput{}, fmt.Errorf("after: %w; call again without after to take any request", err)
		}
		if prev.EndpointID != in.EndpointID {
			return nil, WaitForRequestOutput{}, errors.New("after must be the id of a request this endpoint received")
		}
		since = cursor{id: prev.ID, at: prev.CreatedAt}
	}

	deadline := time.Now().Add(timeout)
	base := "/api/endpoints/" + url.PathEscape(in.EndpointID) + "/requests"
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, WaitForRequestOutput{Requests: []map[string]any{}, TimedOut: true}, nil
		}
		wait := min(remaining, maxServerWait)
		q := url.Values{"limit": {"50"}, "wait": {strconv.Itoa(int(wait.Round(time.Second) / time.Second))}}
		if in.After != "" {
			q.Set("after", in.After)
		}
		callCtx, cancel := context.WithTimeout(ctx, wait+15*time.Second)
		started := time.Now()
		var out ListRequestsOutput
		err := c.do(callCtx, http.MethodGet, base+"?"+q.Encode(), nil, &out)
		cancel()
		if err != nil {
			return nil, WaitForRequestOutput{}, err
		}
		if reqs := since.filter(out.Requests); len(reqs) > 0 {
			return nil, WaitForRequestOutput{Requests: reqs}, nil
		}
		// A Testhooks that honours wait answers only when the time is up; one
		// that doesn't answers at once, so pause before asking again.
		if time.Since(started) < wait/2 {
			select {
			case <-ctx.Done():
				return nil, WaitForRequestOutput{}, ctx.Err()
			case <-time.After(min(pollInterval, time.Until(deadline))):
			}
		}
	}
}

// cursor is the request the caller has already seen.
type cursor struct {
	id string
	at time.Time
}

// filter keeps the requests newer than the cursor. Requests captured in the
// same instant are kept, so one may be seen twice but none is missed.
func (c cursor) filter(reqs []map[string]any) []map[string]any {
	if c.id == "" {
		return reqs
	}
	out := make([]map[string]any, 0, len(reqs))
	for _, r := range reqs {
		id, _ := r["id"].(string)
		at, _ := r["created_at"].(string)
		t, err := time.Parse(time.RFC3339Nano, at)
		if id != c.id && (err != nil || !t.Before(c.at)) {
			out = append(out, r)
		}
	}
	return out
}

// --- get_request

type GetRequestInput struct {
	ID string `json:"id" jsonschema:"the request id"`
}

// capturedRequest is the part of a captured request replay needs.
type capturedRequest struct {
	EndpointID   string              `json:"endpoint_id"`
	Method       string              `json:"method"`
	Path         string              `json:"path"`
	Headers      map[string][]string `json:"headers"`
	Query        map[string][]string `json:"query"`
	Body         string              `json:"body"`
	BodyEncoding string              `json:"body_encoding"`
}

func (c *client) getRequest(ctx context.Context, _ *mcp.CallToolRequest, in GetRequestInput) (*mcp.CallToolResult, map[string]any, error) {
	if in.ID == "" {
		return nil, nil, errors.New("id is required")
	}
	var out map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/requests/"+url.PathEscape(in.ID), nil, &out); err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

// --- replay_request

type ReplayRequestInput struct {
	ID string `json:"id" jsonschema:"the id of the captured request to send again"`
}

type ReplayRequestOutput struct {
	URL    string `json:"url" jsonschema:"where the request was sent"`
	Status int    `json:"status" jsonschema:"the status code Testhooks answered with, as the original sender would see it"`
	Body   string `json:"body" jsonschema:"the reply body, cut at 64 KB"`
}

// skipOnReplay are headers that describe the original connection, not the
// request, so they are not sent again.
var skipOnReplay = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true,
	"Te": true, "Trailers": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
	"Host": true, "Content-Length": true,
}

func (c *client) replayRequest(ctx context.Context, _ *mcp.CallToolRequest, in ReplayRequestInput) (*mcp.CallToolResult, ReplayRequestOutput, error) {
	if in.ID == "" {
		return nil, ReplayRequestOutput{}, errors.New("id is required")
	}
	var cr capturedRequest
	if err := c.do(ctx, http.MethodGet, "/api/requests/"+url.PathEscape(in.ID), nil, &cr); err != nil {
		return nil, ReplayRequestOutput{}, err
	}
	var ep Endpoint
	if err := c.do(ctx, http.MethodGet, "/api/endpoints/"+url.PathEscape(cr.EndpointID), nil, &ep); err != nil {
		return nil, ReplayRequestOutput{}, err
	}

	target := c.captureURL(ep.Slug)
	if cr.Path != "" && cr.Path != "/" {
		target += "/" + strings.TrimLeft(cr.Path, "/")
	}
	if q := url.Values(cr.Query).Encode(); q != "" {
		target += "?" + q
	}

	body := []byte(cr.Body)
	if cr.BodyEncoding == "base64" {
		b, err := base64.StdEncoding.DecodeString(cr.Body)
		if err != nil {
			return nil, ReplayRequestOutput{}, fmt.Errorf("decoding stored body: %w", err)
		}
		body = b
	}
	method := cr.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, ReplayRequestOutput{}, err
	}
	for k, vs := range cr.Headers {
		if skipOnReplay[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// No Testhooks auth token here: the capture URL is public, and the
	// request keeps the Authorization header it was captured with, if any.
	res, err := c.http.Do(req)
	if err != nil {
		return nil, ReplayRequestOutput{}, fmt.Errorf("sending to %s: %w", target, err)
	}
	defer res.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	return nil, ReplayRequestOutput{URL: target, Status: res.StatusCode, Body: string(reply)}, nil
}
