package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sarathsp06/testhooks/internal/db"
)

// headerMatcher matches header names exactly or, for patterns ending in "*",
// by prefix. Matching ignores case.
type headerMatcher struct {
	exact    map[string]bool
	prefixes []string
}

func newHeaderMatcher(patterns []string) headerMatcher {
	m := headerMatcher{exact: make(map[string]bool)}
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if prefix != "" {
				m.prefixes = append(m.prefixes, prefix)
			}
		} else if p != "" {
			m.exact[p] = true
		}
	}
	return m
}

func (m headerMatcher) match(name string) bool {
	name = strings.ToLower(name)
	if m.exact[name] {
		return true
	}
	for _, p := range m.prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// strip deletes every matching header from h.
func (m headerMatcher) strip(h http.Header) {
	for name := range h {
		if m.match(name) {
			delete(h, name)
		}
	}
}

// empty reports whether m matches nothing.
func (m headerMatcher) empty() bool {
	return len(m.exact) == 0 && len(m.prefixes) == 0
}

// shown returns req as the UI and API show it: a copy without the matching
// headers, and HiddenHeaders counting them. req itself is not changed.
func (m headerMatcher) shown(req *db.CapturedRequest) *db.CapturedRequest {
	if m.empty() || len(req.Headers) == 0 {
		return req
	}
	var h http.Header
	if err := json.Unmarshal(req.Headers, &h); err != nil {
		return req
	}
	n := len(h)
	m.strip(h)
	if len(h) == n {
		return req
	}
	out := *req
	out.Headers, _ = json.Marshal(h)
	out.HiddenHeaders = n - len(h)
	return &out
}
