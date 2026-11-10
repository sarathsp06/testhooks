package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeAPI is just enough of the Testhooks REST API and capture route for the tools.
type fakeAPI struct {
	mu       sync.Mutex
	config   map[string]any // stored config of endpoint ep1
	patched  map[string]any // last PATCH body
	captured *http.Request  // last request to /h/...
	body     string         // its body
	auth     []string       // Authorization headers seen on /api
}

const endpointJSON = `{"id":"ep1","slug":"abc123","name":"stripe test","mode":"server","created_at":"2026-10-16T23:03:47Z","config":%s}`

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/api/") {
		f.auth = append(f.auth, r.Header.Get("Authorization"))
	}
	w.Header().Set("Content-Type", "application/json")
	cfg, _ := json.Marshal(f.config)
	ep := strings.Replace(endpointJSON, "%s", string(cfg), 1)
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/endpoints":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["name"] != "stripe test" {
			http.Error(w, `{"error":"unexpected name"}`, 400)
			return
		}
		w.WriteHeader(201)
		io.WriteString(w, strings.Replace(endpointJSON, "%s", "{}", 1))
	case r.Method == "GET" && r.URL.Path == "/api/endpoints":
		io.WriteString(w, `{"endpoints":[`+ep+`],"limit":`+r.URL.Query().Get("limit")+`,"offset":0}`)
	case r.Method == "GET" && (r.URL.Path == "/api/endpoints/ep1" || r.URL.Path == "/api/slug/abc123"):
		io.WriteString(w, ep)
	case r.Method == "PATCH" && r.URL.Path == "/api/endpoints/ep1":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.patched = in
		f.config = in["config"].(map[string]any)
		cfg, _ := json.Marshal(f.config)
		io.WriteString(w, strings.Replace(endpointJSON, "%s", string(cfg), 1))
	case r.Method == "GET" && r.URL.Path == "/api/requests/req1":
		io.WriteString(w, `{"id":"req1","endpoint_id":"ep1","method":"PUT","path":"/events/1",
			"headers":{"Content-Type":["application/json"],"Connection":["keep-alive"],"Host":["old.example"],"Webhook-Id":["msg_1"],"Content-Length":["17"]},
			"query":{"debug":["1"]},"body":"{\"hello\":\"world\"}","body_encoding":"text"}`)
	case r.Method == "GET" && r.URL.Path == "/api/endpoints/ep1/requests":
		io.WriteString(w, `{"requests":[{"id":"req1","method":"PUT"}],"total":1,"limit":`+r.URL.Query().Get("limit")+`,"offset":0}`)
	case r.Method == "DELETE" && r.URL.Path == "/api/endpoints/ep1":
		w.WriteHeader(204)
	case r.Method == "GET" && r.URL.Path == "/api/requests/missing":
		w.WriteHeader(404)
		io.WriteString(w, `{"error":"request not found"}`)
	case strings.HasPrefix(r.URL.Path, "/h/"):
		b, _ := io.ReadAll(r.Body)
		f.captured, f.body = r, string(b)
		w.WriteHeader(202)
		io.WriteString(w, `{"status":"ok"}`)
	default:
		w.WriteHeader(404)
		io.WriteString(w, `{"error":"not found"}`)
	}
}

// connect starts the MCP server against a fake API and returns a client session.
func connect(t *testing.T, f *fakeAPI) (*mcp.ClientSession, string) {
	t.Helper()
	api := httptest.NewServer(f)
	t.Cleanup(api.Close)
	return connectTo(t, api.URL+"/", "s3cret"), api.URL
}

// connectTo starts the MCP server against the Testhooks at base.
func connectTo(t *testing.T, base, token string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := NewServer(base, token, "test").Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call runs a tool and decodes its structured output into out.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if out != nil && !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s: decoding output %s: %v", name, b, err)
		}
	}
	return res
}

func errText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestCreateEndpointReturnsCaptureURL(t *testing.T) {
	f := &fakeAPI{config: map[string]any{}}
	cs, base := connect(t, f)
	var ep Endpoint
	res := call(t, cs, "create_endpoint", map[string]any{"name": "stripe test"}, &ep)
	if res.IsError {
		t.Fatal(errText(res))
	}
	if ep.ID != "ep1" || ep.CaptureURL != base+"/h/abc123" {
		t.Fatalf("got %+v", ep)
	}
	if f.auth[0] != "Bearer s3cret" {
		t.Fatalf("auth header = %q", f.auth[0])
	}

	res = call(t, cs, "create_endpoint", map[string]any{"mode": "local"}, nil)
	if !res.IsError || !strings.Contains(errText(res), "server or browser") {
		t.Fatalf("bad mode accepted: %v", errText(res))
	}
}

func TestListAndGetEndpoint(t *testing.T) {
	f := &fakeAPI{config: map[string]any{"forward_url": "https://example.com"}}
	cs, base := connect(t, f)

	var list ListEndpointsOutput
	if res := call(t, cs, "list_endpoints", map[string]any{"limit": 5}, &list); res.IsError {
		t.Fatal(errText(res))
	}
	if len(list.Endpoints) != 1 || list.Endpoints[0].CaptureURL != base+"/h/abc123" {
		t.Fatalf("got %+v", list)
	}

	for _, args := range []map[string]any{{"id": "ep1"}, {"slug": "abc123"}} {
		var ep Endpoint
		if res := call(t, cs, "get_endpoint", args, &ep); res.IsError {
			t.Fatal(errText(res))
		}
		if ep.Config["forward_url"] != "https://example.com" || ep.CaptureURL != base+"/h/abc123" {
			t.Fatalf("%v: got %+v", args, ep)
		}
	}

	if res := call(t, cs, "get_endpoint", map[string]any{}, nil); !res.IsError {
		t.Fatal("expected an error without id or slug")
	}
}

func TestUpdateConfigMergesIntoStoredConfig(t *testing.T) {
	f := &fakeAPI{config: map[string]any{
		"forward_url": "https://old.example/hook",
		"wasm_script": "return req",
		"signature":   map[string]any{"secret_set": true, "enforce": false},
	}}
	cs, _ := connect(t, f)

	var ep Endpoint
	res := call(t, cs, "update_endpoint_config", map[string]any{
		"id": "ep1",
		"config": map[string]any{
			"forward_url": "https://new.example/hook",
			"wasm_script": nil,
			"signature":   map[string]any{"enforce": true},
		},
	}, &ep)
	if res.IsError {
		t.Fatal(errText(res))
	}
	want := map[string]any{
		"forward_url": "https://new.example/hook",
		"signature":   map[string]any{"secret_set": true, "enforce": true},
	}
	if !reflect.DeepEqual(f.patched["config"], want) {
		t.Fatalf("PATCH config = %v, want %v", f.patched["config"], want)
	}
	if _, ok := f.patched["name"]; ok {
		t.Fatal("PATCH should send only config")
	}
	if ep.Config["forward_url"] != "https://new.example/hook" {
		t.Fatalf("got %+v", ep)
	}
}

func TestReplayRequestSendsToCaptureURL(t *testing.T) {
	f := &fakeAPI{config: map[string]any{}}
	cs, base := connect(t, f)

	var out ReplayRequestOutput
	res := call(t, cs, "replay_request", map[string]any{"id": "req1"}, &out)
	if res.IsError {
		t.Fatal(errText(res))
	}
	if out.Status != 202 || out.URL != base+"/h/abc123/events/1?debug=1" || out.Body != `{"status":"ok"}` {
		t.Fatalf("got %+v", out)
	}
	r := f.captured
	if r.Method != "PUT" || r.URL.Path != "/h/abc123/events/1" || r.URL.RawQuery != "debug=1" {
		t.Fatalf("replayed %s %s", r.Method, r.URL)
	}
	if f.body != `{"hello":"world"}` {
		t.Fatalf("body = %q", f.body)
	}
	if r.Header.Get("Webhook-Id") != "msg_1" || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("headers not kept: %v", r.Header)
	}
	if r.Header.Get("Connection") == "keep-alive" || r.Host == "old.example" {
		t.Fatalf("hop-by-hop headers sent: host=%s %v", r.Host, r.Header)
	}
	if r.Header.Get("Authorization") != "" {
		t.Fatal("the Testhooks token must not be sent to the capture URL")
	}

	res = call(t, cs, "replay_request", map[string]any{"id": "missing"}, nil)
	if !res.IsError || !strings.Contains(errText(res), "request not found") {
		t.Fatalf("got %q", errText(res))
	}
}

func TestListAndGetRequestAndDelete(t *testing.T) {
	f := &fakeAPI{config: map[string]any{}}
	cs, api := connect(t, f)

	var req map[string]any
	if res := call(t, cs, "get_request", map[string]any{"id": "req1"}, &req); res.IsError {
		t.Fatal(errText(res))
	}
	if req["method"] != "PUT" {
		t.Fatalf("got %v", req)
	}
	var list ListRequestsOutput
	if res := call(t, cs, "list_requests", map[string]any{"endpoint_id": "ep1", "limit": 10}, &list); res.IsError {
		t.Fatal(errText(res))
	}
	if list.Total != 1 || len(list.Requests) != 1 || list.Requests[0]["id"] != "req1" {
		t.Fatalf("got %+v", list)
	}
	var sent SendRequestOutput
	if res := call(t, cs, "send_request", map[string]any{"endpoint_id": "ep1", "path": "/stripe/events", "query": map[string]any{"debug": "1"}, "body": `{"event":"x"}`, "headers": map[string]any{"X-Test": "yes"}}, &sent); res.IsError {
		t.Fatal(errText(res))
	}
	if sent.Status != 202 || sent.URL != api+"/h/abc123/stripe/events?debug=1" || sent.Request["id"] != "req1" {
		t.Fatalf("got %+v", sent)
	}
	if f.captured.Method != "POST" || f.captured.Header.Get("Content-Type") != "application/json" || f.captured.Header.Get("X-Test") != "yes" || f.body != `{"event":"x"}` {
		t.Fatalf("sent %s %v body %q", f.captured.Method, f.captured.Header, f.body)
	}
	if !strings.HasPrefix(sent.Curl, "curl -X 'POST' '"+api+"/h/abc123/stripe/events?debug=1'") || !strings.Contains(sent.Curl, `-H 'X-Test: yes'`) || !strings.Contains(sent.Curl, `--data-raw '{"event":"x"}'`) {
		t.Fatalf("curl = %q", sent.Curl)
	}
	var got map[string]any
	if res := call(t, cs, "get_request", map[string]any{"id": "req1"}, &got); res.IsError {
		t.Fatal(errText(res))
	}
	if got["capture_url"] != api+"/h/abc123" || !strings.Contains(got["curl"].(string), "-H 'Webhook-Id: msg_1'") || strings.Contains(got["curl"].(string), "Host:") {
		t.Fatalf("get_request curl = %v %v", got["capture_url"], got["curl"])
	}
	var del DeleteEndpointOutput
	if res := call(t, cs, "delete_endpoint", map[string]any{"id": "ep1"}, &del); res.IsError || del.Deleted != "ep1" {
		t.Fatalf("got %q %+v", errText(res), del)
	}
	if res := call(t, cs, "delete_endpoint", map[string]any{"id": "nope"}, nil); !res.IsError || !strings.Contains(errText(res), "404") {
		t.Fatalf("got %q", errText(res))
	}
}

func TestMergeConfig(t *testing.T) {
	base := map[string]any{"a": 1, "n": map[string]any{"x": 1, "y": 2}}
	got := mergeConfig(base, map[string]any{"a": nil, "b": 2, "n": map[string]any{"y": nil, "z": 3}})
	want := map[string]any{"b": 2, "n": map[string]any{"x": 1, "z": 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if base["a"] != 1 || len(base["n"].(map[string]any)) != 2 {
		t.Fatal("base was modified")
	}
}

// waitAPI is a Testhooks whose request list is empty until a request is added,
// honouring wait and after like the real API when honourWait is set, and
// answering at once (an older server) when it is not.
type waitAPI struct {
	mu          sync.Mutex
	honourWait  bool
	reqs        []string // JSON, newest first
	calls       int
	lastQuery   url.Values
	addedSignal chan struct{}
}

func (f *waitAPI) add(req string) {
	f.mu.Lock()
	f.reqs = append([]string{req}, f.reqs...)
	f.mu.Unlock()
	select {
	case f.addedSignal <- struct{}{}:
	default:
	}
}

func (f *waitAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/requests/req0":
		io.WriteString(w, `{"id":"req0","endpoint_id":"ep1","created_at":"2026-11-10T10:00:00Z"}`)
	case r.URL.Path == "/api/requests/other":
		io.WriteString(w, `{"id":"other","endpoint_id":"ep2","created_at":"2026-11-10T10:00:00Z"}`)
	case r.URL.Path == "/api/endpoints/ep1/requests":
		f.mu.Lock()
		f.calls++
		f.lastQuery = r.URL.Query()
		f.mu.Unlock()
		after := r.URL.Query().Get("after")
		current := func() []string {
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.honourWait || after != "req0" {
				return f.reqs
			}
			var kept []string
			for _, q := range f.reqs {
				if !strings.Contains(q, `"id":"req0"`) {
					kept = append(kept, q)
				}
			}
			return kept
		}
		reqs := current()
		if f.honourWait {
			wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
			timeout := time.After(time.Duration(wait) * time.Second)
		waiting:
			for len(reqs) == 0 {
				select {
				case <-f.addedSignal: // may be stale, from an add before this call
					reqs = current()
				case <-timeout:
					break waiting
				}
			}
		}
		io.WriteString(w, `{"requests":[`+strings.Join(reqs, ",")+`],"total":`+strconv.Itoa(len(reqs))+`}`)
	default:
		w.WriteHeader(404)
		io.WriteString(w, `{"error":"not found"}`)
	}
}

func newWaitAPI(honourWait bool) (*waitAPI, *httptest.Server) {
	f := &waitAPI{honourWait: honourWait, addedSignal: make(chan struct{}, 1)}
	return f, httptest.NewServer(f)
}

const newReq = `{"id":"req1","endpoint_id":"ep1","method":"POST","created_at":"2026-11-10T10:00:05Z"}`
const oldReq = `{"id":"req0","endpoint_id":"ep1","method":"POST","created_at":"2026-11-10T10:00:00Z"}`

func TestWaitForRequest_ServerHonoursWait(t *testing.T) {
	f, srv := newWaitAPI(true)
	defer srv.Close()
	cs := connectTo(t, srv.URL, "")
	f.add(oldReq)
	go func() {
		time.Sleep(100 * time.Millisecond)
		f.add(newReq)
	}()
	var out WaitForRequestOutput
	started := time.Now()
	if res := call(t, cs, "wait_for_request", map[string]any{"endpoint_id": "ep1", "after": "req0", "timeout_seconds": 10}, &out); res.IsError {
		t.Fatal(errText(res))
	}
	if out.TimedOut || len(out.Requests) != 1 || out.Requests[0]["id"] != "req1" {
		t.Fatalf("got %+v", out)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("waited for the timeout instead of the request")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls != 1 || f.lastQuery.Get("wait") != "10" || f.lastQuery.Get("after") != "req0" {
		t.Fatalf("calls %d query %v; want one call with wait=10 after=req0", f.calls, f.lastQuery)
	}
}

func TestWaitForRequest_PollsAnOlderServer(t *testing.T) {
	f, srv := newWaitAPI(false)
	defer srv.Close()
	cs := connectTo(t, srv.URL, "")
	f.add(oldReq) // an older server ignores after and returns this too
	go func() {
		time.Sleep(1500 * time.Millisecond)
		f.add(newReq)
	}()
	var out WaitForRequestOutput
	if res := call(t, cs, "wait_for_request", map[string]any{"endpoint_id": "ep1", "after": "req0", "timeout_seconds": 10}, &out); res.IsError {
		t.Fatal(errText(res))
	}
	if out.TimedOut || len(out.Requests) != 1 || out.Requests[0]["id"] != "req1" {
		t.Fatalf("got %+v; want only req1, filtered on the client", out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls < 2 {
		t.Fatalf("calls = %d; want the tool to have asked again", f.calls)
	}
}

func TestWaitForRequest_TimesOut(t *testing.T) {
	_, srv := newWaitAPI(false)
	defer srv.Close()
	cs := connectTo(t, srv.URL, "")
	var out WaitForRequestOutput
	started := time.Now()
	if res := call(t, cs, "wait_for_request", map[string]any{"endpoint_id": "ep1", "timeout_seconds": 1}, &out); res.IsError {
		t.Fatal(errText(res))
	}
	if !out.TimedOut || len(out.Requests) != 0 {
		t.Fatalf("got %+v", out)
	}
	if d := time.Since(started); d < 900*time.Millisecond || d > 3*time.Second {
		t.Fatalf("took %v, want about 1s", d)
	}
}

func TestWaitForRequest_BadAfter(t *testing.T) {
	_, srv := newWaitAPI(true)
	defer srv.Close()
	cs := connectTo(t, srv.URL, "")
	if res := call(t, cs, "wait_for_request", map[string]any{"endpoint_id": "ep1", "after": "gone"}, nil); !res.IsError || !strings.Contains(errText(res), "without after") {
		t.Fatalf("got %q", errText(res))
	}
	if res := call(t, cs, "wait_for_request", map[string]any{"endpoint_id": "ep1", "after": "other"}, nil); !res.IsError || !strings.Contains(errText(res), "this endpoint") {
		t.Fatalf("got %q", errText(res))
	}
	if res := call(t, cs, "wait_for_request", map[string]any{}, nil); !res.IsError || !strings.Contains(errText(res), "endpoint_id") {
		t.Fatalf("got %q", errText(res))
	}
}
