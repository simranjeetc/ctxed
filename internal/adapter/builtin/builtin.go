// Package builtin registers the adapters shipped with ctxed. Import it for
// side effects to populate the adapter registry.
package builtin

import (
	// Register the adapters via side effects so adapter.Detect finds them.
	_ "github.com/simranjeetc/ctxed/internal/adapter/claude"
	_ "github.com/simranjeetc/ctxed/internal/adapter/opencode"
)
