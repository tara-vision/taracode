package tools

import (
	"net"
	"testing"
)

func TestPrivateAddressesAreRefused(t *testing.T) {
	private := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv4(10, 1, 2, 3), net.IPv4(172, 16, 0, 1), net.IPv4(169, 254, 169, 254),
		net.IPv4(100, 64, 0, 1), net.IPv4(0, 0, 0, 0), net.ParseIP("::1"), net.ParseIP("fd00::1"), net.ParseIP("fe80::1")}
	for _, ip := range private {
		if !isPrivateIP(ip) {
			t.Errorf("%s must be refused", ip)
		}
	}
	public := []net.IP{net.IPv4(93, 184, 216, 34), net.IPv4(8, 8, 8, 8), net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")}
	for _, ip := range public {
		if isPrivateIP(ip) {
			t.Errorf("%s must be allowed", ip)
		}
	}
}
