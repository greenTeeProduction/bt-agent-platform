package security

import "testing"

func TestSecurityListenerAddress(t *testing.T) {
	for _, tc := range []struct {
		host, key, want string
		fail            bool
	}{
		{"", "", "127.0.0.1:9800", false}, {"127.0.0.1", "", "127.0.0.1:9800", false},
		{"::1", "", "[::1]:9800", false}, {"localhost", "", "127.0.0.1:9800", false},
		{"0.0.0.0", "", "", true}, {"::", "", "", true}, {"192.0.2.1", "", "", true},
		{"0.0.0.0", "fixture-key", "0.0.0.0:9800", false}, {"::", "fixture-key", "[::]:9800", false},
		{"example.com", "fixture-key", "", true}, {"127.0.0.1:9800", "fixture-key", "", true},
	} {
		got, err := ListenerAddress(tc.host, "9800", tc.key)
		if (err != nil) != tc.fail || got != tc.want {
			t.Errorf("host=%q key configured=%t: got %q %v want %q fail=%t", tc.host, tc.key != "", got, err, tc.want, tc.fail)
		}
	}
}
