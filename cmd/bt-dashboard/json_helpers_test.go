package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestEncodeJSON_WritesEncodedValueWithTrailingNewline(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"map", map[string]string{"status": "ok"}, `{"status":"ok"}` + "\n"},
		{"slice", []int{1, 2, 3}, "[1,2,3]\n"},
		{"empty slice", []map[string]string{}, "[]\n"},
		{"nil", nil, "null\n"},
		{"string", "hello", "\"hello\"\n"},
		{"struct", struct {
			Name string `json:"name"`
			N    int    `json:"n"`
		}{"agent", 3}, `{"name":"agent","n":3}` + "\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := encodeJSON(rec, tc.in); err != nil {
				t.Fatalf("encodeJSON returned error: %v", err)
			}
			if got := rec.Body.String(); got != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncodeJSON_DoesNotSetContentTypeHeaderItself(t *testing.T) {
	// encodeJSON never calls w.Header().Set("Content-Type", ...) — every
	// production caller sets it before invoking encodeJSON. When nothing
	// sets it first, httptest.ResponseRecorder (like the real net/http
	// server) auto-sniffs one on the first Write, landing on
	// "text/plain; charset=utf-8" for JSON bytes.
	rec := httptest.NewRecorder()
	if err := encodeJSON(rec, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("encodeJSON returned error: %v", err)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q (sniffed default when unset)", got, want)
	}
}

func TestEncodeJSON_PreservesExplicitContentTypeHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	if err := encodeJSON(rec, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("encodeJSON returned error: %v", err)
	}
	if got, want := rec.Header().Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q (caller-set header preserved)", got, want)
	}
}

func TestEncodeJSON_ReturnsErrorForUnsupportedType(t *testing.T) {
	rec := httptest.NewRecorder()
	err := encodeJSON(rec, make(chan int))
	if err == nil {
		t.Fatal("expected error encoding an unsupported type (chan int), got nil")
	}
	if _, ok := err.(*json.UnsupportedTypeError); !ok {
		t.Errorf("expected *json.UnsupportedTypeError, got %T: %v", err, err)
	}
}
