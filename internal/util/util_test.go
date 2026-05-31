package util

import (
	"testing"
)

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{42, "42"},
		{9999, "9999"},
		{-100, "-100"},
	}
	for _, tt := range tests {
		got := Itoa(tt.n)
		if got != tt.want {
			t.Errorf("Itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hello world", 11, "hello world"},
		{"hello world", 3, "..."},
		{"hi", 3, "hi"},
		{"a", 3, "a"},
		{"ab", 3, "ab"},
		{"abc", 3, "abc"},
		{"abcdef", 5, "ab..."},
	}
	for _, tt := range tests {
		got := Truncate(tt.s, tt.n)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

func TestContainsAnyStr(t *testing.T) {
	tests := []struct {
		s       string
		substrs []string
		want    bool
	}{
		{"hello world", []string{"hello"}, true},
		{"hello world", []string{"xyzzy"}, false},
		{"finance and banking", []string{"finance", "bank", "money"}, true},
		{"nothing here", []string{"xyzzy", "plugh"}, false},
		{"", []string{"x"}, false},
		{"test", []string{}, false},
	}
	for _, tt := range tests {
		got := ContainsAnyStr(tt.s, tt.substrs...)
		if got != tt.want {
			t.Errorf("ContainsAnyStr(%q, %v) = %v, want %v", tt.s, tt.substrs, got, tt.want)
		}
	}
}
