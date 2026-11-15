package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sarathsp06/testhooks/internal/db"
)

func typesCall(t *testing.T, mux *http.ServeMux, method, path, body string) (int, typesResult, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(w, req)
	var out typesResult
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out, w.Body.String()
}

func TestTypes_ForBody(t *testing.T) {
	mux := newMux(newTestAPI(newMockStore()))
	code, out, body := typesCall(t, mux, "POST", "/api/types", `{"body":"{\"type\":\"invoice.paid\",\"amount\":10}"}`)
	if code != 200 || out.Lang != "typescript" || out.Name != "InvoicePaidPayload" || out.From != 1 || !strings.Contains(out.Code, "export interface InvoicePaidPayload") {
		t.Fatalf("got %d %s", code, body)
	}
	code, out, _ = typesCall(t, mux, "POST", "/api/types", `{"lang":"py","name":"Order","bodies":["{\"id\":1}","{\"id\":2,\"paid\":true}"]}`)
	if code != 200 || out.Lang != "pydantic" || out.From != 2 || !strings.Contains(out.Code, "class Order(BaseModel):") || !strings.Contains(out.Code, "paid: bool | None = None") {
		t.Fatalf("got %d %+v", code, out)
	}
	for _, tc := range []struct{ body, msg string }{
		{`{}`, "give body"},
		{`{"body":"not json"}`, "not JSON"},
		{`{"lang":"rust","body":"{}"}`, "unknown language"},
		{`nope`, "invalid JSON"},
	} {
		if code, _, body := typesCall(t, mux, "POST", "/api/types", tc.body); code/100 != 4 || !strings.Contains(body, tc.msg) {
			t.Errorf("%s: got %d %s", tc.body, code, body)
		}
	}
}

func TestTypes_ForRequestAndEndpoint(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "abc", "test", "server", json.RawMessage(`{}`))
	store.seedEndpoint("ep-2", "def", "empty", "server", json.RawMessage(`{}`))
	for _, b := range []string{`{"type":"order.created","id":1,"note":"x"}`, `{"type":"order.paid","id":2}`, `<xml/>`} {
		_ = store.InsertRequest(t.Context(), &db.CapturedRequest{EndpointID: "ep-1", Method: "POST", Path: "/", Body: []byte(b), ContentType: "application/json"})
	}
	mux := newMux(newTestAPI(store))

	code, out, body := typesCall(t, mux, "GET", "/api/requests/req-1/types?lang=go", "")
	if code != 200 || out.Lang != "go" || out.Name != "OrderCreatedPayload" || !strings.Contains(out.Code, "type OrderCreatedPayload struct") {
		t.Fatalf("got %d %s", code, body)
	}
	if code, _, body := typesCall(t, mux, "GET", "/api/requests/nope/types", ""); code != 404 || !strings.Contains(body, "request not found") {
		t.Fatalf("got %d %s", code, body)
	}

	// The endpoint type merges the JSON bodies, newest first, and skips the XML one.
	code, out, body = typesCall(t, mux, "GET", "/api/endpoints/ep-1/types?lang=zod", "")
	if code != 200 || out.From != 2 || !strings.HasPrefix(out.Name, "Order") || !strings.Contains(out.Code, "note: z.string().optional(),") || !strings.Contains(out.Code, "id: z.number(),") || !strings.Contains(out.Code, "inferred from 2 requests") {
		t.Fatalf("got %d %s", code, body)
	}
	code, out, _ = typesCall(t, mux, "GET", "/api/endpoints/ep-1/types?last=1&name=Latest", "")
	if code != 200 || out.From != 1 || out.Name != "Latest" {
		t.Fatalf("last=1: got %d %+v", code, out)
	}
	if code, _, body := typesCall(t, mux, "GET", "/api/endpoints/ep-1/types?last=0", ""); code != 400 || !strings.Contains(body, "1 to 200") {
		t.Fatalf("last=0: got %d %s", code, body)
	}
	if code, _, body := typesCall(t, mux, "GET", "/api/endpoints/ep-2/types", ""); code != 404 || !strings.Contains(body, "no requests with a JSON body") {
		t.Fatalf("empty endpoint: got %d %s", code, body)
	}
	if code, _, _ := typesCall(t, mux, "GET", "/api/endpoints/nope/types", ""); code != 404 {
		t.Fatalf("unknown endpoint: got %d", code)
	}
}
