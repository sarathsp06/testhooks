// Package skills holds the agent skill files shipped with Testhooks, so the
// binary can print the one that matches it (testhooks skill).
package skills

import _ "embed"

// Testhooks is skills/testhooks/SKILL.md: how an agent should use Testhooks.
//
//go:embed testhooks/SKILL.md
var Testhooks string
