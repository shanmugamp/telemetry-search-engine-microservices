package indexer

import (
	"slices"
	"testing"
)

// ── stop words ────────────────────────────────────────────────────────────────

func TestTokenize_StopWords_Filtered(t *testing.T) {
	result := Tokenize("a is the and or")
	if len(result) != 0 {
		t.Errorf("all stop words should be filtered, got %v", result)
	}
}

func TestTokenize_StopWords_Mixed(t *testing.T) {
	result := Tokenize("hello a world")
	if !slices.Contains(result, "hello") {
		t.Error("expected 'hello'")
	}
	if !slices.Contains(result, "world") {
		t.Error("expected 'world'")
	}
	if slices.Contains(result, "a") {
		t.Error("stop word 'a' must be filtered")
	}
}

// ── case normalisation ────────────────────────────────────────────────────────

func TestTokenize_LowerCase(t *testing.T) {
	result := Tokenize("HELLO World")
	if !slices.Contains(result, "hello") {
		t.Error("expected lowercase 'hello'")
	}
	if !slices.Contains(result, "world") {
		t.Error("expected lowercase 'world'")
	}
}

// ── plain words ───────────────────────────────────────────────────────────────

func TestTokenize_PlainWords(t *testing.T) {
	result := Tokenize("kafka nginx error")
	for _, want := range []string{"kafka", "nginx", "error"} {
		if !slices.Contains(result, want) {
			t.Errorf("expected token %q in %v", want, result)
		}
	}
}

func TestTokenize_SingleAlphaNum(t *testing.T) {
	// Non-stop-word single characters must be tokenized
	for _, tok := range []string{"b", "1", "x", "i", "_"} {
		result := Tokenize(tok)
		if !slices.Contains(result, tok) {
			t.Errorf("Tokenize(%q) should contain %q, got %v", tok, tok, result)
		}
	}
}

func TestTokenize_SingleChar_StopWord(t *testing.T) {
	result := Tokenize("a")
	if slices.Contains(result, "a") {
		t.Error("stop word 'a' should be filtered even as single char")
	}
}

// ── compound identifiers (the fluent-bit regression) ─────────────────────────

// TestTokenize_FluentBit_FullToken ensures "fluent-bit" is kept as one token.
// This is the primary search token that makes ?q=fluent-bit work.
func TestTokenize_FluentBit_FullToken(t *testing.T) {
	result := Tokenize("fluent-bit")
	if !slices.Contains(result, "fluent-bit") {
		t.Errorf("'fluent-bit' must be present as full compound token; got %v", result)
	}
}

// TestTokenize_FluentBit_SubToken_Fluent ensures sub-parts are also indexed so
// that a user can search just "fluent" and still find "fluent-bit" documents.
func TestTokenize_FluentBit_SubToken_Fluent(t *testing.T) {
	result := Tokenize("fluent-bit")
	if !slices.Contains(result, "fluent") {
		t.Errorf("sub-token 'fluent' must be indexed for partial matching; got %v", result)
	}
}

func TestTokenize_FluentBit_SubToken_Bit(t *testing.T) {
	result := Tokenize("fluent-bit")
	if !slices.Contains(result, "bit") {
		t.Errorf("sub-token 'bit' must be indexed for partial matching; got %v", result)
	}
}

// TestTokenize_QueryTokensMatchDocTokens verifies that tokenizing the search
// query "fluent-bit" produces tokens that appear in the doc token set, so BM25
// lookup will resolve the match correctly.
func TestTokenize_QueryTokensMatchDocTokens(t *testing.T) {
	docTokens := Tokenize("fluent-bit")
	queryTokens := Tokenize("fluent-bit")
	for _, qt := range queryTokens {
		if !slices.Contains(docTokens, qt) {
			t.Errorf("query token %q not found in doc tokens %v", qt, docTokens)
		}
	}
}

func TestTokenize_HyphenatedHostnames(t *testing.T) {
	cases := []struct {
		input   string
		full    string
		subToks []string
	}{
		{"fluent-bit", "fluent-bit", []string{"fluent", "bit"}},
		{"log-shipper", "log-shipper", []string{"log", "shipper"}},
		{"kafka-broker-01", "kafka-broker-01", []string{"kafka", "broker", "01"}},
		{"web-server-prod", "web-server-prod", []string{"web", "server", "prod"}},
	}
	for _, tc := range cases {
		result := Tokenize(tc.input)
		if !slices.Contains(result, tc.full) {
			t.Errorf("%q: full token %q missing; got %v", tc.input, tc.full, result)
		}
		for _, sub := range tc.subToks {
			if !slices.Contains(result, sub) {
				t.Errorf("%q: sub-token %q missing; got %v", tc.input, sub, result)
			}
		}
	}
}

func TestTokenize_DotSeparated(t *testing.T) {
	result := Tokenize("kafka-broker_01.prod")
	if !slices.Contains(result, "kafka-broker_01.prod") {
		t.Errorf("full compound token missing; got %v", result)
	}
	for _, sub := range []string{"kafka", "prod"} {
		if !slices.Contains(result, sub) {
			t.Errorf("sub-token %q missing; got %v", sub, result)
		}
	}
}

func TestTokenize_IPAddress(t *testing.T) {
	result := Tokenize("10.0.0.1")
	if !slices.Contains(result, "10.0.0.1") {
		t.Errorf("IP address should be kept as single token; got %v", result)
	}
}

// ── no junk tokens ────────────────────────────────────────────────────────────

func TestTokenize_NoStandaloneDash(t *testing.T) {
	result := Tokenize("word - another")
	if slices.Contains(result, "-") {
		t.Errorf("standalone '-' must not become a token; got %v", result)
	}
}

func TestTokenize_NoStandaloneDot(t *testing.T) {
	result := Tokenize("word . another")
	if slices.Contains(result, ".") {
		t.Errorf("standalone '.' must not become a token; got %v", result)
	}
}

// ── deduplication ─────────────────────────────────────────────────────────────

func TestTokenize_NoDuplicates(t *testing.T) {
	result := Tokenize("error error error")
	count := 0
	for _, tok := range result {
		if tok == "error" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("duplicate tokens must be deduplicated; got %d 'error' tokens in %v", count, result)
	}
}

func TestTokenize_CompoundNoDuplicateSubTokens(t *testing.T) {
	// "fluent-bit fluent bit" — "fluent" and "bit" appear as sub-tokens AND
	// as standalone words; each must appear exactly once after deduplication.
	result := Tokenize("fluent-bit fluent bit")
	for _, want := range []string{"fluent", "bit", "fluent-bit"} {
		count := 0
		for _, tok := range result {
			if tok == want {
				count++
			}
		}
		if count != 1 {
			t.Errorf("token %q should appear exactly once, got %d; tokens=%v", want, count, result)
		}
	}
}
