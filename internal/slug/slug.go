// Package slug makes the short names endpoints are reached by: /h/{slug} for
// capture and /{slug} for the endpoint page.
//
// A slug is three random words, optionally after a name the user typed:
// "stripe-acid-fable-gully" or "acid-fable-gully". The words come from the
// EFF short word list #1 (1,296 words of at most five letters; 1,295 are used, chosen to be
// easy to read and type), embedded so the binary needs no word file. Because
// three words are always appended, a typed name can never produce a slug that
// clashes with an app route or with another endpoint's whole slug by design;
// a random clash is caught by the store and retried.
//
// Slugs are for reading, not secrecy: 1,295³ is about 2.2 billion, which keeps
// collisions rare but is not meant to stop someone guessing a URL.
package slug

import (
	"crypto/rand"
	_ "embed"
	"errors"
	"math/big"
	"strings"
)

// eff_short_wordlist_1.txt is © Electronic Frontier Foundation, CC BY 3.0.
// See NOTICE at the repository root.
//
//go:embed eff_short_wordlist_1.txt
var wordFile string

// words is the list in file order, without "yo-yo" (the list also has
// "yoyo"), so every word is letters only and a slug's dashes always separate
// words.
var words = func() []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(wordFile), "\n") {
		_, w, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if !strings.Contains(w, "-") {
			out = append(out, w)
		}
	}
	return out
}()

// MaxNameLen is the longest name a user may put in front of the words.
const MaxNameLen = 20

// ErrInvalidName is returned by New and CleanName for a name that can't go in
// a URL as typed. Its text is shown to users as is.
var ErrInvalidName = errors.New("Use up to 20 lowercase letters, numbers and single dashes, with no dash at the start or end.")

// New returns a fresh slug: name (cleaned by CleanName) followed by three
// random words, or just the three words when name is empty.
func New(name string) (string, error) {
	name, err := CleanName(name)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, 4)
	if name != "" {
		parts = append(parts, name)
	}
	for range 3 {
		parts = append(parts, randomWord())
	}
	return strings.Join(parts, "-"), nil
}

// CleanName trims and lowercases name and checks it: letters a-z, digits and
// single dashes, no dash at either end, at most MaxNameLen characters. An
// empty name is valid and means "no name".
func CleanName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", nil
	}
	if len(name) > MaxNameLen || name[0] == '-' || name[len(name)-1] == '-' || strings.Contains(name, "--") {
		return "", ErrInvalidName
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return "", ErrInvalidName
		}
	}
	return name, nil
}

func randomWord() string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
	if err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return words[n.Int64()]
}
