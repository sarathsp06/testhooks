package mcp

import (
	"context"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultURL is used when TESTHOOKS_URL is not set.
const DefaultURL = "http://localhost:8080"

// Run serves MCP over stdin/stdout until the client disconnects or ctx ends.
// It talks to the Testhooks at TESTHOOKS_URL, with TESTHOOKS_AUTH_TOKEN as
// the bearer token when set.
func Run(ctx context.Context, version string) error {
	base := os.Getenv("TESTHOOKS_URL")
	if base == "" {
		base = DefaultURL
	}
	return NewServer(base, os.Getenv("TESTHOOKS_AUTH_TOKEN"), version).Run(ctx, &mcp.StdioTransport{})
}

// NewServer returns an MCP server whose tools call the Testhooks API at base.
func NewServer(base, token, version string) *mcp.Server {
	c := newClient(base, token)
	s := mcp.NewServer(&mcp.Implementation{Name: "testhooks", Title: "Testhooks", Version: version}, &mcp.ServerOptions{
		Instructions: "Testhooks gives you webhook URLs that record every request sent to them. " +
			"Create an endpoint, give its capture_url to a webhook provider (or send to it yourself), " +
			"then list and read the requests that arrived to see exactly what the provider sends.",
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
		Name:        "list_requests",
		Description: "List the requests an endpoint has received, newest first, with their method, headers and body.",
		Annotations: read,
	}, c.listRequests)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_request",
		Description: "Show one received request in full: method, path, query, headers, body, and what was forwarded and replied.",
		Annotations: read,
	}, c.getRequest)
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
