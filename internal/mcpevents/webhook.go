// Package mcpevents implements the MCP Events webhook lifecycle. The owner
// supplies authorization and source data; this package never forwards Core APIs.
package mcpevents

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func publicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func callbackURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || len(value) > 4096 {
		return fmt.Errorf("invalid callback URL")
	}
	if addr, err := netip.ParseAddr(u.Hostname()); err == nil && !publicAddress(addr) {
		return fmt.Errorf("non-public callback address")
	}
	return nil
}

func webhookClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			// No environment proxy: validate the actual destination for every
			// connection, then dial that address without a second DNS lookup.
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
				if err != nil {
					return nil, err
				}
				if len(addresses) == 0 {
					return nil, fmt.Errorf("callback DNS returned no addresses")
				}
				for _, addr := range addresses {
					if !publicAddress(addr) {
						return nil, fmt.Errorf("callback DNS returned a non-public address")
					}
				}
				var last error
				for _, addr := range addresses {
					conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(addr.String(), port))
					if err == nil {
						return conn, nil
					}
					last = err
				}
				return nil, last
			},
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 8 * time.Second,
		},
	}
}

func secretKey(secret string) ([]byte, error) {
	if !strings.HasPrefix(secret, "whsec_") {
		return nil, fmt.Errorf("expected whsec_ signing secret")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil || len(key) < 24 || len(key) > 64 {
		return nil, fmt.Errorf("signing key must decode to 24–64 bytes")
	}
	return key, nil
}

func signature(secret, id, stamp string, body []byte) string {
	key, _ := secretKey(secret)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(id + "." + stamp + "."))
	_, _ = mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func postWebhook(ctx context.Context, client *http.Client, sub Subscription, id string, body []byte) (int, []byte, error) {
	if len(body) > 256<<10 {
		return 413, nil, fmt.Errorf("event exceeds 256 KiB")
	}
	if err := callbackURL(sub.URL); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", sub.URL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signature(sub.Secret, id, stamp, body)
	if sub.PreviousSecret != "" && time.Now().Before(sub.RotateUntil) {
		sig += " " + signature(sub.PreviousSecret, id, stamp, body)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", stamp)
	req.Header.Set("webhook-signature", sig)
	req.Header.Set("X-MCP-Subscription-Id", sub.ID)
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8193))
	if len(data) > 8192 {
		return res.StatusCode, nil, fmt.Errorf("callback response too large")
	}
	return res.StatusCode, data, err
}
