//go:build ignore

// Vercel's Go preset won't build a repo unless main.go, cmd/api/main.go or
// cmd/server/main.go exists, even when vercel.json's buildCommand does the
// build. This file only satisfies that check; the real server is
// ./cmd/testhooks, built by the buildCommand. The build tag keeps it out of
// go build ./... and go vet.
package main

func main() {}
