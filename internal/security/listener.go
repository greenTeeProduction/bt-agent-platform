package security

import (
	"fmt"
	"net"
)

// ListenerAddress defaults to loopback and requires credentials for an
// explicitly configured remote bind. Literal IPs avoid DNS-dependent exposure.
func ListenerAddress(host, port, apiKey string) (string, error) {
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("bind host must be a literal IP address or localhost")
	}
	if !ip.IsLoopback() && apiKey == "" {
		return "", fmt.Errorf("remote bind requires a configured platform API key (BT_API_KEY)")
	}
	return net.JoinHostPort(host, port), nil
}
