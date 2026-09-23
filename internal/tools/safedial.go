package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// allowLocalFetch is switched on only by tests that serve from 127.0.0.1.
var allowLocalFetch = false

// carrierGradeNAT is 100.64.0.0/10, which net.IP.IsPrivate does not cover.
var carrierGradeNAT = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isPrivateIP reports addresses web_fetch must never reach: loopback, RFC 1918 and ULA ranges,
// link-local (including the cloud metadata address), carrier-grade NAT, multicast, unspecified.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || carrierGradeNAT.Contains(ip)
}

// safeTransport resolves the host itself, refuses private addresses, and dials the checked address
// so a DNS answer cannot change between the check and the connection. Every redirect hop goes
// through it too.
func safeTransport(timeout time.Duration) *http.Transport {
	dialer := &net.Dialer{Timeout: timeout}
	return &http.Transport{
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no address for %s", host)
			}
			for _, ip := range ips {
				if isPrivateIP(ip.IP) && !allowLocalFetch {
					return nil, fmt.Errorf("web_fetch refuses %s: %s is a private or local address", host, ip.IP)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
}
