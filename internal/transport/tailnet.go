package transport

import (
	"context"
	"net/netip"
	"sort"
	"strings"

	"tailscale.com/client/local"
)

type TailnetStatus struct {
	Running bool
	DNSName string
	IPs     []netip.Addr
}

type TailnetStatusProvider interface {
	Status(context.Context) (TailnetStatus, error)
}

type LocalAPITailnetStatus struct {
	Client *local.Client
}

// Status uses the stable Tailscale LocalAPI. Every error represents ordinary
// unavailability to callers; it must never make a LAN or Tailcat share fail.
func (provider LocalAPITailnetStatus) Status(ctx context.Context) (TailnetStatus, error) {
	client := provider.Client
	if client == nil {
		client = new(local.Client)
	}
	status, err := client.StatusWithoutPeers(ctx)
	if err != nil {
		return TailnetStatus{}, err
	}
	result := TailnetStatus{Running: status.BackendState == "Running"}
	if !result.Running || status.Self == nil {
		return result, nil
	}
	result.DNSName = strings.TrimSpace(status.Self.DNSName)
	seen := map[netip.Addr]bool{}
	for _, addr := range status.Self.TailscaleIPs {
		addr = addr.Unmap()
		if !addr.IsValid() || seen[addr] {
			continue
		}
		seen[addr] = true
		result.IPs = append(result.IPs, addr)
	}
	sort.Slice(result.IPs, func(i, j int) bool { return result.IPs[i].Compare(result.IPs[j]) < 0 })
	return result, nil
}
