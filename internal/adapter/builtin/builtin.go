// Package builtin registers the adapters shipped with ctxed. Import it for
// side effects to populate the adapter registry.
package builtin

import (
	_ "github.com/simranjeetc/ctxed/internal/adapter/claude"
	_ "github.com/simranjeetc/ctxed/internal/adapter/opencode"
)
