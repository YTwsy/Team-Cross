package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	MDNSService = "_teamcross._tcp"
	MDNSDomain  = "local."
)

var privateIPv4Prefixes = [...]netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

func IsRFC1918(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.Is4() {
		return false
	}
	for _, prefix := range privateIPv4Prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// LANEndpoints returns explicit invitation candidates. mDNS is only a
// discovery accelerator; these addresses let join work when multicast is
// filtered by a Wi-Fi access point.
func LANEndpoints(port int) ([]string, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port %d", port)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var endpoints []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			host := address.String()
			if slash := strings.LastIndexByte(host, '/'); slash >= 0 {
				host = host[:slash]
			}
			addr, err := netip.ParseAddr(host)
			if err != nil || !IsRFC1918(addr) {
				continue
			}
			endpoint := net.JoinHostPort(addr.String(), strconv.Itoa(port))
			if !seen[endpoint] {
				seen[endpoint] = true
				endpoints = append(endpoints, endpoint)
			}
		}
	}
	sort.Strings(endpoints)
	return endpoints, nil
}

type mdnsServer struct {
	server *zeroconf.Server
	once   sync.Once
}

func (s *mdnsServer) Close() error {
	s.once.Do(s.server.Shutdown)
	return nil
}

// PublishMDNS announces one ephemeral Share, never its secret.
func PublishMDNS(instance string, port int, text []string) (io.Closer, error) {
	if strings.TrimSpace(instance) == "" {
		return nil, errors.New("mDNS instance is required")
	}
	server, err := zeroconf.Register(instance, MDNSService, MDNSDomain, port, text, nil)
	if err != nil {
		return nil, fmt.Errorf("publish mDNS: %w", err)
	}
	return &mdnsServer{server: server}, nil
}

// BrowseMDNS collects RFC1918 endpoints for an invitation's exact instance.
// The caller controls the discovery window with ctx.
func BrowseMDNS(ctx context.Context, instance string) ([]string, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("create mDNS resolver: %w", err)
	}
	entries := make(chan *zeroconf.ServiceEntry, 16)
	if err := resolver.Browse(ctx, MDNSService, MDNSDomain, entries); err != nil {
		return nil, fmt.Errorf("browse mDNS: %w", err)
	}

	seen := map[string]bool{}
	var endpoints []string
	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				sort.Strings(endpoints)
				return endpoints, nil
			}
			if entry == nil || (instance != "" && entry.Instance != instance) {
				continue
			}
			for _, ip := range entry.AddrIPv4 {
				addr, ok := netip.AddrFromSlice(ip)
				if !ok || !IsRFC1918(addr) {
					continue
				}
				endpoint := net.JoinHostPort(addr.String(), strconv.Itoa(entry.Port))
				if !seen[endpoint] {
					seen[endpoint] = true
					endpoints = append(endpoints, endpoint)
				}
			}
		case <-ctx.Done():
			sort.Strings(endpoints)
			return endpoints, nil
		}
	}
}

// BrowseFor bounds discovery independently from the caller's longer phase.
func BrowseFor(ctx context.Context, instance string, duration time.Duration) ([]string, error) {
	discoveryCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	return BrowseMDNS(discoveryCtx, instance)
}
