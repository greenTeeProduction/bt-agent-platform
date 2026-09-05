// Package util provides shared utility functions used across the bt-evolve codebase.
package util

import (
	"strconv"
	"strings"
)

// Itoa converts an int to its decimal string representation.
// This is a thin wrapper around strconv.Itoa, provided as a single
// canonical helper to eliminate duplicate reimplementations across packages.
func Itoa(n int) string { return strconv.Itoa(n) }

// Truncate shortens s to at most n characters, appending "..." if
// truncation occurs. The n parameter includes the 3 dots, so the
// returned string is at most n characters long.
// If len(s) <= n, s is returned unchanged.
// If n <= 3, "..." is returned for any non-empty s.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return "..."
	}
	return s[:n-3] + "..."
}

// ContainsAnyStr reports whether s contains any of the given substrings.
// Returns false if substrs is empty.
func ContainsAnyStr(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ContainsAnyFold reports whether s contains any of the given substrings,
// ignoring ASCII/Unicode case on both sides. It is the case-insensitive
// counterpart to ContainsAnyStr, intended for keyword routing where the input
// casing is unknown (e.g. "URGENT: fix prod" must still match "urgent").
// Returns false if substrs is empty.
func ContainsAnyFold(s string, substrs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
