package apiclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

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

func TestDo_OlderServerWithoutTheRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, "<!doctype html><title>Testhooks</title>")
	}))
	defer srv.Close()
	c := New(srv.URL, "")
	var out map[string]any
	err := c.Do(context.Background(), http.MethodPost, "/api/endpoints/x/preview", map[string]any{}, &out)
	if err == nil || !strings.Contains(err.Error(), "text/html, not JSON") || !strings.Contains(err.Error(), "older than this client") {
		t.Fatalf("got %v", err)
	}
}
