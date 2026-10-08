// Package ctxed exposes the assets that ship inside the ctxed binary.
package ctxed

import _ "embed"

// OverviewSkill is the ctxed-overview skill, written to the agent harnesses by
// `ctxed skill install` and refreshed by `ctxed update`.
//
//go:embed skills/ctxed-overview/SKILL.md
var OverviewSkill []byte
