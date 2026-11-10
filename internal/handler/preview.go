package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sarathsp06/testhooks/internal/config"
	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/wasm"
)

// ScriptRunner runs endpoint scripts. *wasm.Runner satisfies it.
type ScriptRunner interface {
	Ready() bool
	Transform(ctx context.Context, script string, input wasm.TransformInput) (*wasm.TransformOutput, error)
	RunResponseHandler(ctx context.Context, script string, input wasm.TransformInput, fwdResp *wasm.ForwardResponse) (*wasm.ResponseHandlerOutput, error)
}

// SetScriptRunner lets PreviewScript run scripts. Without one, previews are
// answered with 503.
func (a *API) SetScriptRunner(r ScriptRunner) {
	a.scripts = r
}

// previewRequest is the body of POST /api/endpoints/{id}/preview.
type previewRequest struct {
	// RequestID names a captured request to try the scripts on. Request is
	// an inline one instead; exactly one of the two is required.
	RequestID string        `json:"request_id"`
	Request   *previewInput `json:"request"`
	// Transform and Reply are scripts to try. Left out, the endpoint's saved
	// script is used when it would run; "" runs none.
	Transform *string `json:"transform"`
	Reply     *string `json:"reply"`
	// ForwardResponse stands in for a sync forward target's reply, which the
	// reply script sees as req.forward_response.
	ForwardResponse *wasm.ForwardResponse `json:"forward_response"`
}

// previewInput is a request given inline, in the shape the API returns one.
type previewInput struct {
	Method       string              `json:"method"`
	Path         string              `json:"path"`
	Headers      map[string][]string `json:"headers"`
	Query        map[string][]string `json:"query"`
	Body         string              `json:"body"`
	BodyEncoding string              `json:"body_encoding"`
	ContentType  string              `json:"content_type"`
}

// previewResult is what PreviewScript answers with.
type previewResult struct {
	// Input is the req the scripts received.
	Input wasm.TransformInput `json:"input"`
	// Transform is what the transform did, or null when none ran.
	Transform *transformPreview `json:"transform"`
	// Reply is what the sender would get.
	Reply replyPreview `json:"reply"`
}

type transformPreview struct {
	// Forwarded is the request as it would be forwarded, after the transform.
	// Null when the transform dropped the request or threw.
	Forwarded *wasm.TransformInput `json:"forwarded"`
	Dropped   bool                 `json:"dropped"`
	Error     string               `json:"error,omitempty"`
}

type replyPreview struct {
	Status      int               `json:"status"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	// Source says what decided the reply: handler, forward_passthrough or
	// default, as in the response_info event. A reply script that threw
	// falls back to default, with Error set.
	Source string `json:"source"`
	Error  string `json:"error,omitempty"`
}

// PreviewScript handles POST /api/endpoints/{id}/preview: it runs a transform
// and a reply script on a captured or given request and reports what would be
// forwarded and what the sender would get. Nothing is sent and nothing is
// saved, so a caller can see a script's effect before saving it, as the UI's
// editor does.
func (a *API) PreviewScript(w http.ResponseWriter, r *http.Request) {
	ep, err := a.db.GetEndpointByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.writeLookupError(w, err, "endpoint")
		return
	}
	if a.scripts == nil || !a.scripts.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scripts can't run on this server"})
		return
	}

	var in previewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	input, status, msg := a.previewInput(r, ep, in)
	if msg != "" {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}

	epConfig := config.ParseEndpointConfig(ep.Config)
	transform, reply := previewScripts(epConfig, in)

	res := previewResult{Input: input}
	handlerInput := input
	if transform != "" {
		res.Transform = &transformPreview{}
		out, err := a.scripts.Transform(r.Context(), transform, input)
		switch {
		case err != nil:
			res.Transform.Error = err.Error()
		case out.Drop:
			res.Transform.Dropped = true
		default:
			handlerInput = applyTransform(input, out)
			fwd := handlerInput
			res.Transform.Forwarded = &fwd
		}
	}

	res.Reply = replyPreview{Status: http.StatusOK, Body: defaultResponseBody(in.RequestID), ContentType: "application/json", Source: "default"}
	if in.RequestID == "" {
		res.Reply.Body = defaultResponseBody("<request id>")
	}
	if ep.Mode == "browser" {
		res.Reply.Status = http.StatusAccepted
	}
	if res.Transform != nil && res.Transform.Forwarded == nil {
		// Dropped or failed: the sender gets the default reply, as in capture.
		writeJSON(w, http.StatusOK, res)
		return
	}
	if reply != "" {
		out, err := a.scripts.RunResponseHandler(r.Context(), reply, handlerInput, in.ForwardResponse)
		switch {
		case err != nil:
			res.Reply.Error = err.Error()
		case out != nil:
			res.Reply = replyPreview{Status: out.Status, Headers: out.Headers, Body: out.Body, ContentType: out.ContentType, Source: "handler"}
			if res.Reply.Status == 0 {
				res.Reply.Status = http.StatusOK
			}
			if res.Reply.ContentType == "" {
				res.Reply.ContentType = "text/plain"
			}
		}
	} else if in.ForwardResponse != nil {
		fr := in.ForwardResponse
		res.Reply = replyPreview{Status: fr.Status, Headers: fr.Headers, Body: fr.Body, ContentType: fr.ContentType, Source: "forward_passthrough"}
		if res.Reply.Status == 0 {
			res.Reply.Status = http.StatusBadGateway
		}
	}
	writeJSON(w, http.StatusOK, res)
}

// previewInput builds the req the scripts get, from the named captured request
// or the inline one. A non-empty msg is an error to answer with status.
func (a *API) previewInput(r *http.Request, ep *db.Endpoint, in previewRequest) (input wasm.TransformInput, status int, msg string) {
	switch {
	case in.RequestID != "" && in.Request != nil:
		return input, http.StatusBadRequest, "give request_id or request, not both"
	case in.RequestID != "":
		req, err := a.db.GetRequest(r.Context(), in.RequestID)
		if errors.Is(err, db.ErrNotFound) {
			return input, http.StatusNotFound, "request not found"
		}
		if err != nil {
			a.log.Error().Err(err).Msg("preview: load request")
			return input, http.StatusInternalServerError, "internal error"
		}
		if req.EndpointID != ep.ID {
			return input, http.StatusBadRequest, "request_id must be a request this endpoint received"
		}
		req = a.hideHeaders.shown(req) // hidden headers never reach scripts
		input = wasm.TransformInput{Method: req.Method, Path: req.Path, Body: string(req.Body), ContentType: req.ContentType}
		_ = json.Unmarshal(req.Headers, &input.Headers)
		_ = json.Unmarshal(req.Query, &input.Query)
	case in.Request != nil:
		body := in.Request.Body
		if in.Request.BodyEncoding == "base64" {
			b, err := base64.StdEncoding.DecodeString(body)
			if err != nil {
				return input, http.StatusBadRequest, "request.body is not valid base64"
			}
			body = string(b)
		}
		input = wasm.TransformInput{Method: in.Request.Method, Path: in.Request.Path, Headers: in.Request.Headers, Query: in.Request.Query, Body: body, ContentType: in.Request.ContentType}
		if input.Method == "" {
			input.Method = http.MethodPost
		}
		if input.Path == "" {
			input.Path = "/"
		}
		if input.ContentType == "" && input.Headers != nil {
			input.ContentType = http.Header(input.Headers).Get("Content-Type")
		}
	default:
		return input, http.StatusBadRequest, "give request_id (a captured request) or request (one to try)"
	}
	if input.Headers == nil {
		input.Headers = map[string][]string{}
	}
	return input, 0, ""
}

// previewScripts picks the transform and reply scripts to run: the ones given,
// or the endpoint's saved ones when they would run on a real request.
func previewScripts(cfg config.EndpointConfig, in previewRequest) (transform, reply string) {
	if in.Transform != nil {
		transform = *in.Transform
	} else if cfg.WASMScript != "" && !cfg.TransformDisabled && config.IsJavaScript(cfg.TransformLanguage) {
		transform = cfg.WASMScript
	}
	if in.Reply != nil {
		reply = *in.Reply
	} else if cr := cfg.CustomResponse; cr != nil && cr.Enabled && cr.Script != "" && config.IsJavaScript(cr.Language) {
		reply = cr.Script
	}
	return transform, reply
}

// applyTransform returns the request as it would be forwarded, the same way
// capture applies a transform's output.
func applyTransform(in wasm.TransformInput, out *wasm.TransformOutput) wasm.TransformInput {
	if out.Body != "" {
		in.Body = out.Body
	}
	if out.ContentType != "" {
		in.ContentType = out.ContentType
	}
	if out.Headers != nil {
		in.Headers = out.Headers
	}
	if out.Method != "" {
		in.Method = out.Method
	}
	if out.Path != "" {
		in.Path = out.Path
	}
	return in
}
