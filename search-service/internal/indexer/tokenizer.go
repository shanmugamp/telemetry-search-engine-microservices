package indexer

import (
	"maps"
	"regexp"
	"strings"
)

// primaryRegex matches tokens that start AND end with an alphanumeric/underscore
// character, with optional internal hyphens and dots.
// This prevents standalone "-" or "." from becoming junk tokens while keeping
// compound identifiers like "fluent-bit" and "kafka-broker_01.prod" intact.
var primaryRegex = regexp.MustCompile(`[a-zA-Z0-9_][a-zA-Z0-9_\-\.]*[a-zA-Z0-9_]|[a-zA-Z0-9_]`)

// splitRegex splits compound tokens on hyphens and dots.
var splitRegex = regexp.MustCompile(`[\-\.]+`)

// stopWords is a set of common words excluded from indexing.
var stopWords = maps.Clone(map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {},
	"is": {}, "in": {}, "at": {}, "to": {}, "of": {},
	"for": {}, "on": {}, "with": {}, "by": {}, "from": {},
})

// Tokenize lower-cases text, extracts word tokens, and removes stop words.
//
// Compound identifiers like "fluent-bit" and "kafka-broker_01.prod" are kept
// as single tokens AND their hyphen/dot-separated components are also emitted
// individually.  This means:
//
//   - searching "fluent-bit"   → matches docs where hostname is "fluent-bit"
//   - searching "fluent"       → also matches those docs (sub-token)
//   - searching "bit"          → also matches those docs (sub-token)
//
// Duplicate tokens within the same text are deduplicated so the BM25 term
// frequency is not inflated.
func Tokenize(text string) []string {
	raw := primaryRegex.FindAllString(strings.ToLower(text), -1)

	seen := make(map[string]struct{}, len(raw)*2)
	out := make([]string, 0, len(raw)*2)

	add := func(t string) {
		if t == "" {
			return
		}
		if _, stop := stopWords[t]; stop {
			return
		}
		if _, dup := seen[t]; dup {
			return
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}

	for _, tok := range raw {
		// Always index the full compound token (e.g. "fluent-bit").
		add(tok)

		// Also index each hyphen/dot-separated component so that partial
		// queries ("fluent", "bit") resolve correctly.
		if strings.ContainsAny(tok, "-.") {
			for _, part := range splitRegex.Split(tok, -1) {
				add(part)
			}
		}
	}

	return out
}
