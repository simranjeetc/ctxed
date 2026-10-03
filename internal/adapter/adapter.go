// Package adapter defines the session-format boundary and the registry of
// supported harnesses.
package adapter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/simranjeetc/ctxed/internal/session"
)

// Adapter reads and writes one harness's session document format.
type Adapter interface {
	Name() string
	Detect(data []byte) bool
	Parse(data []byte) (*session.Document, error)
	Write(doc *session.Document) ([]byte, error)
}

var registry = map[string]Adapter{}

// Register adds an adapter. Concrete adapters call this from init.
func Register(a Adapter) { registry[a.Name()] = a }

// All returns the registered adapters, sorted by name.
func All() []Adapter {
	names := Names()
	out := make([]Adapter, 0, len(names))
	for _, n := range names {
		out = append(out, registry[n])
	}
	return out
}

// Names returns the registered adapter names, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Detect returns the first adapter that recognizes data, or an error naming
// the supported formats.
func Detect(data []byte) (Adapter, error) {
	for _, a := range All() {
		if a.Detect(data) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("unrecognized session format; supported formats: %s", strings.Join(Names(), ", "))
}
