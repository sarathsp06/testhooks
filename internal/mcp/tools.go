package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sarathsp06/testhooks/internal/apiclient"
)

// The tool inputs and outputs are the apiclient's, so their JSON schemas and
// descriptions are written once.
type (
	Endpoint             = apiclient.Endpoint
	CreateEndpointInput  = apiclient.CreateEndpointInput
	ListEndpointsInput   = apiclient.ListEndpointsInput
	ListEndpointsOutput  = apiclient.ListEndpointsOutput
	GetEndpointInput     = apiclient.GetEndpointInput
	UpdateConfigInput    = apiclient.UpdateConfigInput
	DeleteEndpointInput  = apiclient.DeleteEndpointInput
	DeleteEndpointOutput = apiclient.DeleteEndpointOutput
	ListRequestsInput    = apiclient.ListRequestsInput
	ListRequestsOutput   = apiclient.ListRequestsOutput
	WaitForRequestInput  = apiclient.WaitForRequestInput
	WaitForRequestOutput = apiclient.WaitForRequestOutput
	PreviewScriptInput   = apiclient.PreviewScriptInput
	GetRequestInput      = apiclient.GetRequestInput
	SendRequestInput     = apiclient.SendRequestInput
	SendRequestOutput    = apiclient.SendRequestOutput
	ReplayRequestInput   = apiclient.ReplayRequestInput
	ReplayRequestOutput  = apiclient.ReplayRequestOutput
)

// tools adapts apiclient operations to the MCP tool handler signature.
type tools struct{ api *apiclient.Client }

// A failed operation is returned as a Go error, which the SDK turns into a
// tool result with isError set and the message as its text.

func (t tools) createEndpoint(ctx context.Context, _ *mcp.CallToolRequest, in CreateEndpointInput) (*mcp.CallToolResult, Endpoint, error) {
	out, err := t.api.CreateEndpoint(ctx, in)
	return nil, out, err
}

func (t tools) listEndpoints(ctx context.Context, _ *mcp.CallToolRequest, in ListEndpointsInput) (*mcp.CallToolResult, ListEndpointsOutput, error) {
	out, err := t.api.ListEndpoints(ctx, in)
	return nil, out, err
}

func (t tools) getEndpoint(ctx context.Context, _ *mcp.CallToolRequest, in GetEndpointInput) (*mcp.CallToolResult, Endpoint, error) {
	out, err := t.api.GetEndpoint(ctx, in)
	return nil, out, err
}

func (t tools) updateConfig(ctx context.Context, _ *mcp.CallToolRequest, in UpdateConfigInput) (*mcp.CallToolResult, Endpoint, error) {
	out, err := t.api.UpdateConfig(ctx, in)
	return nil, out, err
}

func (t tools) deleteEndpoint(ctx context.Context, _ *mcp.CallToolRequest, in DeleteEndpointInput) (*mcp.CallToolResult, DeleteEndpointOutput, error) {
	out, err := t.api.DeleteEndpoint(ctx, in)
	return nil, out, err
}

func (t tools) listRequests(ctx context.Context, _ *mcp.CallToolRequest, in ListRequestsInput) (*mcp.CallToolResult, ListRequestsOutput, error) {
	out, err := t.api.ListRequests(ctx, in)
	return nil, out, err
}

func (t tools) waitForRequest(ctx context.Context, _ *mcp.CallToolRequest, in WaitForRequestInput) (*mcp.CallToolResult, WaitForRequestOutput, error) {
	out, err := t.api.WaitForRequest(ctx, in)
	return nil, out, err
}

func (t tools) previewScript(ctx context.Context, _ *mcp.CallToolRequest, in PreviewScriptInput) (*mcp.CallToolResult, map[string]any, error) {
	out, err := t.api.PreviewScript(ctx, in)
	return nil, out, err
}

func (t tools) getRequest(ctx context.Context, _ *mcp.CallToolRequest, in GetRequestInput) (*mcp.CallToolResult, map[string]any, error) {
	out, err := t.api.GetRequest(ctx, in)
	return nil, out, err
}

func (t tools) sendRequest(ctx context.Context, _ *mcp.CallToolRequest, in SendRequestInput) (*mcp.CallToolResult, SendRequestOutput, error) {
	out, err := t.api.SendRequest(ctx, in)
	return nil, out, err
}

func (t tools) replayRequest(ctx context.Context, _ *mcp.CallToolRequest, in ReplayRequestInput) (*mcp.CallToolResult, ReplayRequestOutput, error) {
	out, err := t.api.ReplayRequest(ctx, in)
	return nil, out, err
}
