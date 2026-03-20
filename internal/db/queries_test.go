package db

import (
	"encoding/json"
	"testing"
)

func TestIsTextBody(t *testing.T) {
	tests := []struct {
		name        string
		body        []byte
		contentType string
		want        bool
	}{
		{"empty body", nil, "", true},
		{"empty body with content-type", []byte{}, "application/json", true},
		{"text/plain", []byte("hello"), "text/plain", true},
		{"text/html", []byte("<h1>hi</h1>"), "text/html; charset=utf-8", true},
		{"application/json", []byte(`{"a":1}`), "application/json", true},
		{"application/xml", []byte("<x/>"), "application/xml", true},
		{"application/javascript", []byte("var x=1"), "application/javascript", true},
		{"application/x-www-form-urlencoded", []byte("a=1&b=2"), "application/x-www-form-urlencoded", true},
		{"application/graphql", []byte("{ user { id } }"), "application/graphql", true},
		{"+json suffix", []byte(`{}`), "application/vnd.api+json", true},
		{"+xml suffix", []byte("<x/>"), "application/atom+xml", true},
		{"soap+xml", []byte("<s/>"), "application/soap+xml", true},
		{"xhtml+xml", []byte("<html/>"), "application/xhtml+xml", true},
		{"uppercase content-type", []byte("hello"), "Text/Plain", true},
		{"valid utf8 no content-type", []byte("hello world"), "", true},
		{"valid utf8 unknown type", []byte("abc"), "application/octet-stream", true},
		{"binary data", []byte{0x00, 0xff, 0xfe, 0x80, 0x81}, "application/octet-stream", false},
		{"binary data no type", []byte{0x00, 0xff, 0xfe, 0x80}, "", false},
		{"image/png", []byte{0x89, 0x50, 0x4e, 0x47}, "image/png", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTextBody(tt.body, tt.contentType)
			if got != tt.want {
				t.Errorf("isTextBody(%q, %q) = %v, want %v", tt.body, tt.contentType, got, tt.want)
			}
		})
	}
}

func TestCapturedRequest_MarshalJSON_EmptyBody(t *testing.T) {
	r := &CapturedRequest{
		ID:          "test-id",
		EndpointID:  "ep-id",
		Method:      "GET",
		Path:        "/",
		Headers:     json.RawMessage(`{}`),
		ContentType: "",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Empty body should not have body or body_encoding fields
	if _, ok := m["body"]; ok {
		t.Error("expected no 'body' field for empty body")
	}
	if _, ok := m["body_encoding"]; ok {
		t.Error("expected no 'body_encoding' field for empty body")
	}

	// Core fields should still be present
	if m["method"] != "GET" {
		t.Errorf("expected method=GET, got %v", m["method"])
	}
}

func TestCapturedRequest_MarshalJSON_TextBody(t *testing.T) {
	r := &CapturedRequest{
		ID:          "test-id",
		EndpointID:  "ep-id",
		Method:      "POST",
		Path:        "/webhook",
		Headers:     json.RawMessage(`{"Content-Type":["application/json"]}`),
		Body:        []byte(`{"event":"test","value":42}`),
		ContentType: "application/json",
		Size:        27,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Text body should be emitted as-is (not base64)
	body, ok := m["body"].(string)
	if !ok {
		t.Fatalf("expected body to be string, got %T", m["body"])
	}
	if body != `{"event":"test","value":42}` {
		t.Errorf("body mismatch: got %q", body)
	}

	enc, ok := m["body_encoding"].(string)
	if !ok || enc != "text" {
		t.Errorf("expected body_encoding=text, got %v", m["body_encoding"])
	}
}

func TestCapturedRequest_MarshalJSON_BinaryBody(t *testing.T) {
	binaryData := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a} // PNG header

	r := &CapturedRequest{
		ID:          "test-id",
		EndpointID:  "ep-id",
		Method:      "POST",
		Path:        "/upload",
		Headers:     json.RawMessage(`{}`),
		Body:        binaryData,
		ContentType: "image/png",
		Size:        8,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	enc, ok := m["body_encoding"].(string)
	if !ok || enc != "base64" {
		t.Errorf("expected body_encoding=base64, got %v", m["body_encoding"])
	}

	// body should be a base64 string (not raw bytes)
	body, ok := m["body"].(string)
	if !ok {
		t.Fatalf("expected body to be string, got %T", m["body"])
	}
	if body == "" {
		t.Error("expected non-empty base64 body")
	}
}

func TestCapturedRequest_MarshalJSON_Roundtrip(t *testing.T) {
	// Verify that the standard fields survive marshal/unmarshal
	r := &CapturedRequest{
		ID:          "abc-123",
		EndpointID:  "ep-456",
		Method:      "PUT",
		Path:        "/api/data",
		Headers:     json.RawMessage(`{"X-Custom":["val"]}`),
		Query:       json.RawMessage(`{"page":["1"]}`),
		Body:        []byte("plain text body"),
		ContentType: "text/plain",
		IP:          "192.168.1.1",
		Size:        15,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if m["id"] != "abc-123" {
		t.Errorf("id mismatch: %v", m["id"])
	}
	if m["method"] != "PUT" {
		t.Errorf("method mismatch: %v", m["method"])
	}
	if m["path"] != "/api/data" {
		t.Errorf("path mismatch: %v", m["path"])
	}
	if m["ip"] != "192.168.1.1" {
		t.Errorf("ip mismatch: %v", m["ip"])
	}
	if m["content_type"] != "text/plain" {
		t.Errorf("content_type mismatch: %v", m["content_type"])
	}
	// size comes back as float64 from JSON
	if m["size"] != float64(15) {
		t.Errorf("size mismatch: %v", m["size"])
	}
	if m["body"] != "plain text body" {
		t.Errorf("body mismatch: %v", m["body"])
	}
	if m["body_encoding"] != "text" {
		t.Errorf("body_encoding mismatch: %v", m["body_encoding"])
	}
}
