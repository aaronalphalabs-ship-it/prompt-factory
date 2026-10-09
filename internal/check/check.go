// Package check validates generated copy against marketplace length limits
// and the brand's banned-word list.
package check

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Issue struct {
	Row    int    `json:"row"`
	Field  string `json:"field"`
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
}

// Length checks a value against limits["<platform>.<field>"] in characters.
func Length(limits map[string]int, key, value string) *Issue {
	max, ok := limits[key]
	if !ok {
		return nil
	}
	n := utf8.RuneCountInString(value)
	if n > max {
		return &Issue{Field: key, Rule: "length", Detail: fmt.Sprintf("%d chars > %d", n, max)}
	}
	return nil
}

// Banned reports any banned word present (case-insensitive substring match).
func Banned(words []string, field, value string) []Issue {
	low := strings.ToLower(value)
	var out []Issue
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if strings.Contains(low, strings.ToLower(w)) {
			out = append(out, Issue{Field: field, Rule: "banned_word", Detail: fmt.Sprintf("contains %q", w)})
		}
	}
	return out
}

// Truncate cuts value to max runes on a word boundary when possible.
func Truncate(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	r := []rune(value)[:max]
	s := string(r)
	if i := strings.LastIndexAny(s, " ,;-|"); i > max/2 {
		s = s[:i]
	}
	return strings.TrimRight(s, " ,;-|")
}
