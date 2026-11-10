// Package mcp is a stdio MCP server that lets an AI agent drive a running
// Testhooks. Every tool is an apiclient operation, the same ones the CLI
// offers, so both say and do the same things.
package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sarathsp06/testhooks/internal/apiclient"
)

// Run serves MCP over stdin/stdout until the client disconnects or ctx ends.
// It talks to the Testhooks at TESTHOOKS_URL, with TESTHOOKS_AUTH_TOKEN as
// the bearer token when set.
func Run(ctx context.Context, version string) error {
	return NewServer(apiclient.FromEnv(), version).Run(ctx, &mcp.StdioTransport{})
}

// NewServer returns an MCP server whose tools call the Testhooks API through api.
func NewServer(api *apiclient.Client, version string) *mcp.Server {
	c := tools{api}
	s := mcp.NewServer(&mcp.Implementation{Name: "testhooks", Title: "Testhooks", Version: version}, &mcp.ServerOptions{
		Instructions: "Testhooks gives you webhook URLs that record every request sent to them. " +
			"Create an endpoint, give its capture_url to a webhook provider (or send to it yourself), " +
			"then wait_for_request until the provider sends something and read it to see exactly what it sends. " +
			"Pass the id of the last request you saw as after to wait only for newer ones. " +
			"send_request sends a test request yourself; replay_request sends a captured one again.",
	})

	no := false
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_endpoint",
		Description: "Create a new webhook URL that records every request sent to it, and return its capture_url to give to a provider.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, c.createEndpoint)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_endpoints",
		Description: "List the webhook endpoints that exist, newest first, each with its capture_url.",
		Annotations: read,
	}, c.listEndpoints)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_endpoint",
		Description: "Show one endpoint, found by its id or by the slug in its capture URL, with its settings and capture_url.",
		Annotations: read,
	}, c.getEndpoint)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_endpoint_config",
		Description: "Change some of an endpoint's settings (such as forward_url or a script) and keep the rest as they are.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
	}, c.updateConfig)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "preview_script",
		Description: "Try a transform or reply script on a captured request without saving or sending anything: shows the request as it would be forwarded and what the sender would get, or the error the script threw. Do this before saving a script with update_endpoint_config.",
		Annotations: read,
	}, c.previewScript)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_requests",
		Description: "List the requests an endpoint has received, newest first, with their method, headers and body.",
		Annotations: read,
	}, c.listRequests)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "wait_for_request",
		Description: "Wait until an endpoint receives a request and return it, instead of polling list_requests. Returns at once when the endpoint already has requests newer than after.",
		Annotations: read,
	}, c.waitForRequest)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_request",
		Description: "Show one received request in full: method, path, query, headers, body, and what was forwarded and replied.",
		Annotations: read,
	}, c.getRequest)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_request",
		Description: "Send a test request to an endpoint's capture URL, as a provider would, and return what the sender got back and what Testhooks captured. Use it to try an endpoint, a script or your handler before the real provider sends anything.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, c.sendRequest)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "replay_request",
		Description: "Send a received request to its endpoint again, as if the provider sent it twice, and return the reply.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, c.replayRequest)
	yes := true
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_endpoint",
		Description: "Delete an endpoint and every request it received; this cannot be undone.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
	}, c.deleteEndpoint)
	return s
}
