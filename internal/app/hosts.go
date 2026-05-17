package app

import (
	"strconv"
	"strings"
)

// parseHosts splits a comma-separated list of host patterns and expands any
// brace expressions into individual hostnames.
// Examples:
// "my-app.beacon,{foo,bar}.lh"  → ["my-app.beacon", "foo.lh", "bar.lh"]
// "node{1..4}.lh"               → ["node1.lh", "node2.lh", "node3.lh", "node4.lh"]
func parseHosts(label string) []string {
	var result []string

	for _, part := range splitTopLevel(label, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, expandBraces(part)...)
	}

	return result
}

// splitTopLevel splits str by sep, ignoring occurrences of sep inside {} pairs.
func splitTopLevel(str string, sep byte) []string {
	var parts []string

	depth := 0
	start := 0
	for i := 0; i < len(str); i++ {
		switch str[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		default:
			if str[i] == sep && depth == 0 {
				parts = append(parts, str[start:i])
				start = i + 1
			}
		}
	}

	return append(parts, str[start:])
}

// expandBraces performs shell-like brace expansion on str.
// It supports:
//   - alternation: {foo,bar} → "foo", "bar"
//   - numeric ranges: {1..16} → "1", "2", …, "16"
//
// Patterns may be nested and combined with a prefix/suffix.
func expandBraces(str string) []string {
	open := strings.Index(str, "{")
	if open == -1 {
		return []string{str}
	}

	// Find the matching closing brace.
	depth := 0
	close := -1
	for i := open; i < len(str); i++ {
		switch str[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				close = i
			}
		}
		if close != -1 {
			break
		}
	}

	if close == -1 {
		return []string{str} // unmatched — treat as literal
	}

	prefix := str[:open]
	inner := str[open+1 : close]
	suffix := str[close+1:]

	suffixes := expandBraces(suffix)
	middles := braceAlternatives(inner)

	result := make([]string, 0, len(middles)*len(suffixes))

	for _, m := range middles {
		for _, suf := range suffixes {
			result = append(result, prefix+m+suf)
		}
	}

	return result
}

// braceAlternatives returns the expanded list of alternatives for the content
// inside a brace expression.
func braceAlternatives(inner string) []string {
	alts := splitTopLevel(inner, ',')

	// Single token — check for a numeric range like {1..16}.
	if len(alts) == 1 {
		if from, to, ok := parseNumericRange(alts[0]); ok {
			return numericSequence(from, to)
		}

		return expandBraces(alts[0])
	}

	var result []string

	for _, alt := range alts {
		result = append(result, expandBraces(alt)...)
	}

	return result
}

// parseNumericRange parses "N..M" into (from, to, true).
func parseNumericRange(str string) (int, int, bool) {
	i := strings.Index(str, "..")
	if i == -1 {
		return 0, 0, false
	}

	from, err1 := strconv.Atoi(strings.TrimSpace(str[:i]))
	to, err2 := strconv.Atoi(strings.TrimSpace(str[i+2:]))

	if err1 != nil || err2 != nil {
		return 0, 0, false
	}

	return from, to, true
}

// numericSequence returns string integers from..to inclusive.
func numericSequence(from, to int) []string {
	step := 1

	if from > to {
		step = -1
	}

	n := to - from

	if n < 0 {
		n = -n
	}

	result := make([]string, 0, n+1)

	for v := from; ; v += step {
		result = append(result, strconv.Itoa(v))
		if v == to {
			break
		}
	}

	return result
}
