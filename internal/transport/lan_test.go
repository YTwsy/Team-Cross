package transport

import (
	"net/netip"
	"testing"
)

func TestIsRFC1918(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"172.31.255.254", true},
		{"172.32.0.1", false},
		{"192.168.5.9", true},
		{"100.64.0.1", false},
		{"127.0.0.1", false},
		{"fd7a:115c:a1e0::1", false},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			if got := IsRFC1918(netip.MustParseAddr(test.address)); got != test.want {
				t.Fatalf("IsRFC1918(%s) = %v, want %v", test.address, got, test.want)
			}
		})
	}
}
