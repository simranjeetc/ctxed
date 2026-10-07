// Package inspect renders a session's entries read-only.
package inspect

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

// Row is one entry in an inspection report.
type Row struct {
	Index   int    `json:"index"`
	Role    string `json:"role"`
	Kind    string `json:"kind"`
	Tokens  int    `json:"tokens"`
	Preview string `json:"preview"`
}

// TokenizerInfo describes how tokens were counted.
type TokenizerInfo struct {
	Name        string `json:"name"`
	Approximate bool   `json:"approximate"`
}

// Report is the machine-readable form of an inspection.
type Report struct {
	Source       string        `json:"source"`
	Entries      []Row         `json:"entries"`
	TotalEntries int           `json:"totalEntries"`
	TotalTokens  int           `json:"totalTokens"`
	Tokenizer    TokenizerInfo `json:"tokenizer"`
}

// Build compiles the report for a document.
func Build(doc *session.Document, tok tokenize.Tokenizer) Report {
	r := Report{
		Source:    doc.Source,
		Entries:   make([]Row, 0, len(doc.Entries)),
		Tokenizer: TokenizerInfo{Name: tok.Name(), Approximate: tok.Approximate()},
	}
	for _, e := range doc.Entries {
		n := tok.Count(e.Text)
		r.Entries = append(r.Entries, Row{
			Index:   e.Index,
			Role:    e.Role,
			Kind:    string(e.Kind),
			Tokens:  n,
			Preview: e.Preview,
		})
		r.TotalTokens += n
	}
	r.TotalEntries = len(doc.Entries)
	return r
}

// Render writes the report as a table or as JSON.
func Render(w io.Writer, doc *session.Document, tok tokenize.Tokenizer, asJSON bool) error {
	r := Build(doc, tok)
	if asJSON {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", b)
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "IDX\tROLE\tKIND\tTOKENS\tPREVIEW")
	for _, e := range r.Entries {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%s\n", e.Index, e.Role, e.Kind, e.Tokens, e.Preview)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	mode := tok.Name()
	if tok.Approximate() {
		mode += " · approximate"
	} else {
		mode += " · exact"
	}
	_, err := fmt.Fprintf(w, "TOTAL %d entries · %s tokens  (tokenizer: %s)\n",
		r.TotalEntries, human(r.TotalTokens), mode)
	return err
}

func human(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
