// Package tokenize chooses how ctxed counts tokens for an entry.
package tokenize

import (
	"fmt"
	"unicode/utf8"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// Tokenizer counts tokens in text.
type Tokenizer interface {
	Name() string
	Approximate() bool
	Count(text string) int
}

// Approximation is the documented fallback used when no model tokenizer is
// available. It estimates one token per four runes, rounded up — for example,
// the 5-rune string "abcde" counts as 2 tokens.
type Approximation struct{}

// Name reports this tokenizer's name.
func (Approximation) Name() string { return "approximation" }

// Approximate reports that counts are estimates, not exact tokens.
func (Approximation) Approximate() bool { return true }

// Count estimates one token per four runes, rounded up.
func (Approximation) Count(s string) int {
	if s == "" {
		return 0
	}
	return (utf8.RuneCountInString(s) + 3) / 4
}

// Tiktoken wraps a real BPE encoding.
type Tiktoken struct {
	enc  *tiktoken.Tiktoken
	name string
}

// Name reports the tokenizer's name.
func (t Tiktoken) Name() string { return t.name }

// Approximate reports that counts are exact BPE token counts.
func (t Tiktoken) Approximate() bool { return false }

// Count returns the exact number of BPE tokens in s.
func (t Tiktoken) Count(s string) int {
	if s == "" {
		return 0
	}
	return len(t.enc.Encode(s, nil, nil))
}

// Resolve returns the tokenizer for an explicit encoding, else the model's
// tokenizer, else the documented approximation. An explicit encoding that does
// not exist is an error; an unknown model falls back to the labelled
// approximation rather than failing.
func Resolve(model, encoding string) (Tokenizer, error) {
	if encoding != "" {
		enc, err := tiktoken.GetEncoding(encoding)
		if err != nil {
			return nil, fmt.Errorf("unknown tokenizer %q", encoding)
		}
		return Tiktoken{enc: enc, name: encoding}, nil
	}
	if model == "" {
		return Approximation{}, nil
	}
	enc, err := tiktoken.EncodingForModel(model)
	if err != nil {
		return Approximation{}, nil
	}
	return Tiktoken{enc: enc, name: "model:" + model}, nil
}
