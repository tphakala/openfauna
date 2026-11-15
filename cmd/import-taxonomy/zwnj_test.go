package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZWNJTokens_Normalize(t *testing.T) {
	m := zwnjTokens{bareBrown: joinedBrown}
	tests := []struct {
		name, in, want string
		m              zwnjTokens
	}{
		{"bare token replaced", bareBrown, joinedBrown, m},
		{"inside parentheses", "(" + bareBrown + ")", "(" + joinedBrown + ")", m},
		{"next to a hyphen", "x-" + bareBrown, "x-" + joinedBrown, m},
		{"next to a digit", bareBrown + "2", joinedBrown + "2", m},
		{"longer token left alone", bareBrown + "ها", bareBrown + "ها", m},
		{"doubled collapsed", "ا\u200c\u200cب", "ا\u200cب", m},
		{"leading removed", "\u200cاب", "اب", m},
		{"trailing removed", "اب\u200c", "اب", m},
		{"next to a space removed", "اب \u200cاب", "اب اب", m},
		{"well-formed kept", "ا\u200cب", "ا\u200cب", m},
		{"latin untouched", "Common Name", "Common Name", m},
		{"nil map strips only", "\u200c" + bareBrown, bareBrown, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.m.normalize(tc.in)
			if got != tc.want {
				t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if again := tc.m.normalize(got); again != got {
				t.Errorf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestLoadZWNJTokens(t *testing.T) {
	m, err := loadZWNJTokens("../../data/validation/fa_zwnj_tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) == 0 {
		t.Fatal("committed token map is empty")
	}

	bad := map[string]string{
		"no tokens object":         `{"_comment": "x"}`,
		"value changes a letter":   `{"tokens": {"اب": "ا\u200cج"}}`,
		"value equals key":         `{"tokens": {"اب": "اب"}}`,
		"key holds U+200C":         `{"tokens": {"ا\u200cب": "ا\u200cب\u200cج"}}`,
		"key holds a space":        `{"tokens": {"ا ب": "ا\u200c ب"}}`,
		"value has a stray U+200C": `{"tokens": {"اب": "اب\u200c"}}`,
	}
	for name, content := range bad {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "m.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadZWNJTokens(path); err == nil {
				t.Errorf("accepted %s", content)
			}
		})
	}
}
