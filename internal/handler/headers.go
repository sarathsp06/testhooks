package handler

import (
	"net/http"
	"strings"
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
