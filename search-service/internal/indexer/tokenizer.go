package indexer

import (
	"maps"
	"regexp"
	"strings"
)

var tokenRegex = regexp.MustCompile(`[a-zA-Z0-9_\-\.]+`)

// stopWords is a set of common words excluded from indexing.
var stopWords = maps.Clone(map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {},
	"is": {}, "in": {}, "at": {}, "to": {}, "of": {},
	"for": {}, "on": {}, "with": {}, "by": {}, "from": {},
})

// Tokenize lower-cases text, extracts word tokens, and removes stop words.
func Tokenize(text string) []string {
	raw := tokenRegex.FindAllString(strings.ToLower(text), -1)
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if len(t) > 1 {
			if _, stop := stopWords[t]; !stop {
				out = append(out, t)
			}
		}
	}
	return out
}
