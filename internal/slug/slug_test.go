package slug

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestWordList(t *testing.T) {
	if len(words) != 1295 {
		t.Fatalf("len(words) = %d, want 1295 (EFF short word list #1 less yo-yo)", len(words))
	}
	if words[0] != "acid" || words[len(words)-1] != "zoom" {
		t.Errorf("first, last = %q, %q; want acid, zoom", words[0], words[len(words)-1])
	}
	letters := regexp.MustCompile(`^[a-z]{3,5}$`)
	seen := map[string]bool{}
	for _, w := range words {
		if !letters.MatchString(w) {
			t.Errorf("word %q is not 3-5 lowercase letters", w)
		}
		if seen[w] {
			t.Errorf("word %q appears twice", w)
		}
		seen[w] = true
	}
}

func TestNew(t *testing.T) {
	inList := map[string]bool{}
	for _, w := range words {
		inList[w] = true
	}
	for _, tc := range []struct{ name, prefix string }{
		{"", ""},
		{"stripe", "stripe-"},
		{"  My-Shop2 ", "my-shop2-"},
	} {
		s, err := New(tc.name)
		if err != nil {
			t.Fatalf("New(%q): %v", tc.name, err)
		}
		if !strings.HasPrefix(s, tc.prefix) {
			t.Errorf("New(%q) = %q, want prefix %q", tc.name, s, tc.prefix)
		}
		parts := strings.Split(strings.TrimPrefix(s, tc.prefix), "-")
		if len(parts) != 3 {
			t.Fatalf("New(%q) = %q, want three words after the name", tc.name, s)
		}
		for _, p := range parts {
			if !inList[p] {
				t.Errorf("New(%q) = %q: %q is not from the word list", tc.name, s, p)
			}
		}
	}
	if a, b := mustNew(t), mustNew(t); a == b {
		t.Errorf("two slugs in a row are both %q", a)
	}
}

func mustNew(t *testing.T) string {
	s, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCleanName(t *testing.T) {
	ok := map[string]string{
		"":                     "",
		"   ":                  "",
		"stripe":               "stripe",
		"Stripe":               "stripe",
		"a":                    "a",
		"my-shop-2":            "my-shop-2",
		"12345678901234567890": "12345678901234567890",
	}
	for in, want := range ok {
		got, err := CleanName(in)
		if err != nil || got != want {
			t.Errorf("CleanName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"-stripe", "stripe-", "my--shop", "my shop", "my_shop", "shop/x", "../x",
		"café", "a.b", "123456789012345678901", "h?x", "%2e",
	} {
		if _, err := CleanName(in); !errors.Is(err, ErrInvalidName) {
			t.Errorf("CleanName(%q): err = %v, want ErrInvalidName", in, err)
		}
	}
	if _, err := New("bad name"); !errors.Is(err, ErrInvalidName) {
		t.Errorf("New with a bad name: err = %v, want ErrInvalidName", err)
	}
}
