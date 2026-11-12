package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type IPResolver struct {
	trusted []netip.Prefix
}

func NewIPResolver(cidrs string) (IPResolver, error) {
	var resolver IPResolver
	for _, value := range strings.Split(cidrs, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return IPResolver{}, fmt.Errorf("invalid trusted proxy CIDR %q: %w", value, err)
		}
		resolver.trusted = append(resolver.trusted, prefix.Masked())
	}
	return resolver, nil
}

func (resolver IPResolver) ClientIP(request *http.Request) (string, bool) {
	peer, ok := parseIP(request.RemoteAddr)
	if !ok {
		return "", false
	}
	if !resolver.isTrusted(peer) {
		return peer.String(), true
	}

	forwarded := request.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return peer.String(), true
	}
	parts := strings.Split(strings.Join(forwarded, ","), ",")
	current := peer
	for index := len(parts) - 1; index >= 0; index-- {
		candidate, ok := parseIP(strings.TrimSpace(parts[index]))
		if !ok {
			// A malformed chain is untrustworthy. Falling back to the immediate
			// peer groups the traffic conservatively instead of accepting spoofed data.
			return peer.String(), true
		}
		current = candidate
		if !resolver.isTrusted(current) {
			return current.String(), true
		}
	}
	return current.String(), true
}

func (resolver IPResolver) isTrusted(address netip.Addr) bool {
	for _, prefix := range resolver.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseIP(value string) (netip.Addr, bool) {
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	address, err := netip.ParseAddr(strings.Trim(value, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}
