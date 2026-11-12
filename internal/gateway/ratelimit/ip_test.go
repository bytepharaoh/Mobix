package ratelimit

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustsForwardingHeadersOnlyFromConfiguredProxies(t *testing.T) {
	resolver, err := NewIPResolver("10.0.0.0/8, 2001:db8:1::/48")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		remote    string
		forwarded string
		want      string
	}{
		{"untrusted peer cannot spoof", "198.51.100.8:4321", "203.0.113.9", "198.51.100.8"},
		{"trusted proxy exposes client", "10.1.2.3:4321", "203.0.113.9", "203.0.113.9"},
		{"trusted chain is removed right to left", "10.1.2.3:4321", "203.0.113.9, 10.9.8.7", "203.0.113.9"},
		{"malformed chain falls back to peer", "10.1.2.3:4321", "not-an-ip", "10.1.2.3"},
		{"mapped address is normalized", "[::ffff:198.51.100.8]:4321", "203.0.113.9", "198.51.100.8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.remote
			request.Header.Set("X-Forwarded-For", test.forwarded)
			actual, ok := resolver.ClientIP(request)
			if !ok || actual != test.want {
				t.Fatalf("ClientIP() = %q, %v, want %q, true", actual, ok, test.want)
			}
		})
	}
}

func TestNewIPResolverRejectsInvalidCIDR(t *testing.T) {
	if _, err := NewIPResolver("10.0.0.0/8, invalid"); err == nil {
		t.Fatal("invalid trusted proxy CIDR was accepted")
	}
}
