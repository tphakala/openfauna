package main

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	faLocale = "fa"
	zwnj     = '‌'
)

// zwnjTokens maps a Persian token written without a zero-width non-joiner
// (U+200C) to the same letters with it. A token is a maximal run of letters,
// marks and U+200C.
type zwnjTokens map[string]string

// loadZWNJTokens reads the "tokens" object of path and rejects an entry whose
// value is not its key with U+200C inserted between letters, so the map can
// change letter joining but never a letter.
func loadZWNJTokens(path string) (zwnjTokens, error) {
	var doc struct {
		Tokens zwnjTokens `json:"tokens"`
	}
	if err := loadJSONFile(path, &doc); err != nil {
		return nil, err
	}
	if doc.Tokens == nil {
		return nil, fmt.Errorf("%s: no \"tokens\" object", path)
	}
	for k, v := range doc.Tokens {
		if k == v || strings.ContainsFunc(k, func(r rune) bool { return !isLetterOrMark(r) }) ||
			strings.ReplaceAll(v, string(zwnj), "") != k || stripStrayZWNJ(v) != v {
			return nil, fmt.Errorf("%s: %q: the key must be one word of letters and the value the same word with U+200C inserted between letters", path, k)
		}
	}
	return doc.Tokens, nil
}

func isLetterOrMark(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) }

// stripStrayZWNJ removes every U+200C that is not between two letters (leading,
// trailing, next to a space or punctuation) and collapses a run of them to one.
func stripStrayZWNJ(v string) string {
	if !strings.ContainsRune(v, zwnj) {
		return v
	}
	rs := []rune(v)
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		if rs[i] != zwnj {
			out = append(out, rs[i])
			continue
		}
		j := i
		for j < len(rs) && rs[j] == zwnj {
			j++
		}
		if len(out) > 0 && isLetterOrMark(out[len(out)-1]) && j < len(rs) && isLetterOrMark(rs[j]) {
			out = append(out, zwnj)
		}
		i = j - 1
	}
	return string(out)
}

// normalize strips stray U+200C from v and replaces every token that is a key
// of m with its value.
func (m zwnjTokens) normalize(v string) string {
	v = stripStrayZWNJ(v)
	var b strings.Builder
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		tok := v[start:end]
		if repl, ok := m[tok]; ok {
			tok = repl
		}
		b.WriteString(tok)
		start = -1
	}
	for i, r := range v {
		if r == zwnj || isLetterOrMark(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
		b.WriteRune(r)
	}
	flush(len(v))
	return b.String()
}
