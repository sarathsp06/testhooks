package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sarathsp06/testhooks/internal/apiclient"
)

// fakeHooks is enough of a Testhooks for the commands: one endpoint, ep1 at
// /h/abc123, with a request list that starts empty.
type fakeHooks struct {
	mu       sync.Mutex
	requests []string // JSON, newest first
	deleted  []string
	patched  map[string]any
	captured *http.Request
	body     string
}

const ep1 = `{"id":"ep1","slug":"abc123","name":"stripe test","mode":"server","created_at":"2026-11-10T10:00:00Z","config":{"forward_url":"https://old.example"}}`
const req1 = `{"id":"req1","endpoint_id":"ep1","method":"POST","path":"/events","headers":{"Content-Type":["application/json"]},"body":"{\"event\":\"x\"}","body_encoding":"text","size":13,"created_at":"2026-11-10T10:00:05Z"}`

func (f *fakeHooks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/endpoints":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.patched = in
		w.WriteHeader(201)
		io.WriteString(w, ep1)
	case r.Method == "GET" && r.URL.Path == "/api/endpoints":
		io.WriteString(w, `{"endpoints":[`+ep1+`],"limit":50,"offset":0}`)
	case r.Method == "GET" && (r.URL.Path == "/api/endpoints/ep1" || r.URL.Path == "/api/slug/abc123"):
		io.WriteString(w, ep1)
	case r.Method == "PATCH" && r.URL.Path == "/api/endpoints/ep1":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.patched = in
		io.WriteString(w, ep1)
	case r.Method == "DELETE" && r.URL.Path == "/api/endpoints/ep1":
		f.deleted = append(f.deleted, "ep1")
		w.WriteHeader(204)
	case r.Method == "GET" && r.URL.Path == "/api/endpoints/ep1/requests":
		io.WriteString(w, `{"requests":[`+strings.Join(f.requests, ",")+`],"total":`+itoa(len(f.requests))+`,"limit":50,"offset":0}`)
	case r.Method == "GET" && r.URL.Path == "/api/requests/req1":
		io.WriteString(w, req1)
	case r.Method == "POST" && r.URL.Path == "/api/endpoints/ep1/preview":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.patched = in
		io.WriteString(w, `{"input":{"method":"POST","path":"/events"},"transform":{"forwarded":{"method":"POST","path":"/events","body":"changed"},"dropped":false},"reply":{"status":201,"body":"ok","content_type":"text/plain","source":"handler"}}`)
	case strings.HasPrefix(r.URL.Path, "/h/abc123"):
		b, _ := io.ReadAll(r.Body)
		f.captured, f.body = r, string(b)
		f.requests = append([]string{req1}, f.requests...)
		io.WriteString(w, `{"status":"ok","id":"req1"}`)
	default:
		w.WriteHeader(404)
		io.WriteString(w, `{"error":"not found"}`)
	}
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+n))) }

type result struct {
	code        int
	out, errOut string
}

func run(t *testing.T, f *fakeHooks, terminal bool, stdin string, args ...string) result {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	var out, errOut bytes.Buffer
	code := Run(args, Env{
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut, Version: "test",
		IsTerminal: func() bool { return terminal },
		Client:     apiclient.New(srv.URL, "tok"),
	})
	return result{code, out.String(), errOut.String()}
}

func TestCLI_EndpointsJSONAndText(t *testing.T) {
	f := &fakeHooks{}
	r := run(t, f, false, "", "endpoints", "create", "--slug", "stripe", "--name", "stripe test", "--config", `{"forward_url":"https://x"}`)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.errOut)
	}
	var ep apiclient.Endpoint
	if err := json.Unmarshal([]byte(r.out), &ep); err != nil || ep.CaptureURL == "" || !strings.HasSuffix(ep.CaptureURL, "/h/abc123") {
		t.Fatalf("piped output should be JSON with capture_url, got %q (%v)", r.out, err)
	}
	if f.patched["slug_name"] != "stripe" || f.patched["config"].(map[string]any)["forward_url"] != "https://x" {
		t.Fatalf("sent %v", f.patched)
	}

	r = run(t, f, true, "", "endpoints", "create")
	if r.code != 0 || !strings.Contains(r.out, "Send webhooks to:") || !strings.Contains(r.out, "testhooks requests wait ep1") {
		t.Fatalf("terminal output should be text with next step, got %q", r.out)
	}
	r = run(t, f, true, "", "endpoints", "list", "--json")
	if r.code != 0 || !strings.HasPrefix(strings.TrimSpace(r.out), "{") {
		t.Fatalf("--json should force JSON, got %q", r.out)
	}
	r = run(t, f, false, "", "--plain", "endpoints", "list")
	if r.code != 0 || !strings.Contains(r.out, "/h/abc123  ep1  server  stripe test") {
		t.Fatalf("--plain should force text, got %q", r.out)
	}
	// A slug works where an id does.
	r = run(t, f, false, "", "endpoints", "get", "abc123")
	if r.code != 0 || !strings.Contains(r.out, `"id": "ep1"`) {
		t.Fatalf("get by slug: %d %q %s", r.code, r.out, r.errOut)
	}
	r = run(t, f, false, "", "endpoints", "get", "nope")
	if r.code != 1 || !strings.Contains(r.errOut, `no endpoint with id or slug "nope"`) {
		t.Fatalf("unknown: %d %s", r.code, r.errOut)
	}
	r = run(t, f, false, "", "endpoints", "update", "ep1", "--config", `{"forward_url":null,"wasm_script":"return req"}`)
	if r.code != 0 || f.patched["config"].(map[string]any)["forward_url"] != nil || f.patched["config"].(map[string]any)["wasm_script"] != "return req" {
		t.Fatalf("update merged config wrong: %v (%s)", f.patched, r.errOut)
	}
	r = run(t, f, false, "", "endpoints", "update", "ep1", "--mode", "browser")
	if r.code != 0 || f.patched["mode"] != "browser" || f.patched["config"] != nil {
		t.Fatalf("update mode sent %v", f.patched)
	}
}

func TestCLI_DeleteConfirms(t *testing.T) {
	f := &fakeHooks{}
	r := run(t, f, false, "", "endpoints", "delete", "ep1")
	if r.code != 2 || len(f.deleted) != 0 || !strings.Contains(r.errOut, "--yes") {
		t.Fatalf("piped delete without --yes must refuse: %d %s", r.code, r.errOut)
	}
	r = run(t, f, true, "n\n", "endpoints", "delete", "ep1")
	if r.code != 0 || len(f.deleted) != 0 || !strings.Contains(r.errOut, "Kept") {
		t.Fatalf("answering n must keep: %d %s", r.code, r.errOut)
	}
	r = run(t, f, true, "y\n", "endpoints", "delete", "ep1")
	if r.code != 0 || len(f.deleted) != 1 || !strings.Contains(r.out, "Deleted endpoint ep1") {
		t.Fatalf("answering y: %d %s %s", r.code, r.out, r.errOut)
	}
	r = run(t, f, false, "", "endpoints", "delete", "ep1", "--yes")
	if r.code != 0 || len(f.deleted) != 2 || !strings.Contains(r.out, `"deleted": "ep1"`) {
		t.Fatalf("--yes: %d %s %s", r.code, r.out, r.errOut)
	}
}

func TestCLI_RequestsSendWaitPreview(t *testing.T) {
	f := &fakeHooks{}
	r := run(t, f, false, "", "requests", "wait", "abc123", "--timeout", "1")
	if r.code != ExitTimedOut || !strings.Contains(r.out, `"timed_out": true`) {
		t.Fatalf("timed out wait: %d %q %s", r.code, r.out, r.errOut)
	}

	r = run(t, f, false, `{"event":"x"}`, "requests", "send", "ep1", "-p", "/events", "-H", "X-Test: yes", "-d", "-")
	if r.code != 0 {
		t.Fatalf("send: %s", r.errOut)
	}
	var sent apiclient.SendRequestOutput
	_ = json.Unmarshal([]byte(r.out), &sent)
	if sent.Status != 200 || sent.Request["id"] != "req1" || f.captured.Header.Get("X-Test") != "yes" || f.captured.Header.Get("Content-Type") != "application/json" || f.body != `{"event":"x"}` {
		t.Fatalf("send: %+v captured %v %q", sent, f.captured.Header, f.body)
	}

	r = run(t, f, false, "", "requests", "wait", "ep1", "--timeout", "5")
	if r.code != 0 || !strings.Contains(r.out, `"id": "req1"`) || strings.Contains(r.out, `"timed_out": true`) {
		t.Fatalf("wait with a request there: %d %q", r.code, r.out)
	}
	r = run(t, f, true, "", "requests", "list", "ep1")
	if r.code != 0 || !strings.Contains(r.out, "POST   /events") || !strings.Contains(r.out, "req1") {
		t.Fatalf("list text: %q", r.out)
	}
	r = run(t, f, true, "", "requests", "get", "req1")
	if r.code != 0 || !strings.Contains(r.out, "Content-Type: application/json") || !strings.Contains(r.out, `{"event":"x"}`) {
		t.Fatalf("get text: %q", r.out)
	}

	r = run(t, f, false, "function handler(req) { return {status: 201}; }", "requests", "preview", "ep1", "--reply", "-", "--transform", "")
	if r.code != 0 {
		t.Fatalf("preview: %s", r.errOut)
	}
	if f.patched["request_id"] != "req1" || f.patched["transform"] != "" || !strings.Contains(f.patched["reply"].(string), "status: 201") {
		t.Fatalf("preview sent %v", f.patched)
	}
	r = run(t, f, true, "", "requests", "preview", "ep1")
	if r.code != 0 || !strings.Contains(r.out, "Would forward POST /events") || !strings.Contains(r.out, "The sender would get 201 (handler)") {
		t.Fatalf("preview text: %q", r.out)
	}
}

func TestCLI_UsageAndMisc(t *testing.T) {
	f := &fakeHooks{}
	r := run(t, f, false, "", "frobnicate")
	if r.code != 2 || !strings.Contains(r.errOut, `unknown command "frobnicate"`) {
		t.Fatalf("got %d %s", r.code, r.errOut)
	}
	r = run(t, f, false, "")
	if r.code != 2 {
		t.Fatalf("no command: %d", r.code)
	}
	r = run(t, f, false, "", "version")
	if r.code != 0 || r.out != "testhooks test\n" {
		t.Fatalf("version: %q", r.out)
	}
	r = run(t, f, false, "", "skill")
	if r.code != 0 || !strings.HasPrefix(r.out, "---\nname: testhooks\n") {
		t.Fatalf("skill: %q", r.out[:min(len(r.out), 40)])
	}
	r = run(t, f, false, "", "requests", "send")
	if r.code != 2 || !strings.Contains(r.errOut, "give the endpoint") {
		t.Fatalf("missing arg: %d %s", r.code, r.errOut)
	}
}
