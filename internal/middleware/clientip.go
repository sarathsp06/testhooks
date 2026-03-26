// Package middleware provides HTTP middleware components.
//
// ClientIP extracts the real client IP address from an HTTP request,
// accounting for trusted reverse proxies.
package middleware

import (
	"net"
	"strings"
)

// ClientIP extracts the client IP from the request. It only trusts
// X-Forwarded-For / X-Real-IP headers when the direct peer (RemoteAddr)
// is within one of the trustedProxies networks. When trustedProxies is
// empty, proxy headers are ignored and RemoteAddr is always used.
//
// This prevents IP spoofing by untrusted clients setting X-Forwarded-For.
func ClientIP(remoteAddr string, xForwardedFor string, xRealIP string, trustedProxies []*net.IPNet) string {
	// Parse the direct peer IP from RemoteAddr (host:port).
	peerIP := parseIP(remoteAddr)

	// If no trusted proxies configured, never trust proxy headers.
	if len(trustedProxies) == 0 {
		if peerIP != "" {
			return peerIP
		}
		return remoteAddr
	}

	// Check if peer is a trusted proxy.
	if peerIP != "" && isTrustedProxy(peerIP, trustedProxies) {
		// Trusted proxy — check forwarded headers.
		if xff := xForwardedFor; xff != "" {
			// X-Forwarded-For: client, proxy1, proxy2
			// Walk backwards to find the rightmost non-trusted IP.
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				ip := strings.TrimSpace(parts[i])
				if ip == "" {
					continue
				}
				if isTrustedProxy(ip, trustedProxies) {
					continue
				}
				return ip
			}
			// All IPs in the chain are trusted — use the leftmost.
			if first := strings.TrimSpace(parts[0]); first != "" {
				return first
			}
		}
		if xri := xRealIP; xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	// Peer is not trusted or no proxy headers — use RemoteAddr.
	if peerIP != "" {
		return peerIP
	}
	return remoteAddr
}

// parseIP extracts the IP portion from a host:port string.
// M-08: Normalizes IPv6 addresses via net.ParseIP().String() for consistent representation.
func parseIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// May already be a bare IP.
		parsed := net.ParseIP(remoteAddr)
		if parsed != nil {
			return parsed.String() // M-08: normalized form
		}
		return ""
	}
	// M-08: Normalize the extracted IP (handles IPv6 shorthand, zone IDs, etc.)
	parsed := net.ParseIP(ip)
	if parsed != nil {
		return parsed.String()
	}
	return ip
}

// isTrustedProxy checks if the given IP string falls within any trusted CIDR.
func isTrustedProxy(ipStr string, trusted []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range trusted {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
