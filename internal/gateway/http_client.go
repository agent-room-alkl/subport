package gateway

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
	"golang.org/x/net/proxy"
)

// Proxy URL registry keyed by proxy id (from proxies table).
var (
	proxyMu   sync.RWMutex
	proxyURLs = map[string]string{} // id -> URL
)

// SetProxyURL registers or clears an egress proxy URL by id.
func SetProxyURL(proxyID, proxyURL string) {
	proxyID = trimSpace(proxyID)
	if proxyID == "" {
		return
	}
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if trimSpace(proxyURL) == "" {
		delete(proxyURLs, proxyID)
		return
	}
	proxyURLs[proxyID] = trimSpace(proxyURL)
}

// ClearProxyURL removes a proxy registration.
func ClearProxyURL(proxyID string) {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	delete(proxyURLs, proxyID)
}

// NormalizeProxyURL ensures socks5-typed rows dial via SOCKS even when the
// stored URL omits a scheme (common when type is set separately in the UI).
func NormalizeProxyURL(typ, raw string) string {
	raw = trimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" && u.Host != "" {
		return raw
	}
	host := raw
	if strings.HasPrefix(host, "//") {
		host = strings.TrimPrefix(host, "//")
	}
	switch strings.ToLower(trimSpace(typ)) {
	case "socks5", "socks", "socks5h":
		scheme := "socks5"
		if strings.EqualFold(trimSpace(typ), "socks5h") {
			scheme = "socks5h"
		}
		return scheme + "://" + host
	default:
		return "http://" + host
	}
}

// HydrateProxies loads proxy URLs into the process cache.
func HydrateProxies(proxies []model.Proxy) {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	proxyURLs = map[string]string{}
	for _, p := range proxies {
		if !p.Enabled || trimSpace(p.URL) == "" {
			continue
		}
		proxyURLs[p.ID] = NormalizeProxyURL(p.Type, p.URL)
	}
}

func proxyURLForAccount(a model.Account) string {
	id := trimSpace(a.ProxyID)
	if id == "" {
		return ""
	}
	proxyMu.RLock()
	defer proxyMu.RUnlock()
	return proxyURLs[id]
}

// HTTPClientFor returns an HTTP client that routes through the account's
// configured proxy when proxy_id is set and registered. Falls back to the
// shared upstreamClient. Supports http(s) CONNECT proxies and SOCKS5
// (socks5:// / socks5h://), matching proxies.type in the schema.
func HTTPClientFor(a model.Account) *http.Client {
	pu := proxyURLForAccount(a)
	if pu == "" {
		return upstreamClient
	}
	transport, err := transportForProxyURL(pu)
	if err != nil || transport == nil {
		return upstreamClient
	}
	return &http.Client{Timeout: 120 * time.Second, Transport: transport}
}

func transportForProxyURL(pu string) (*http.Transport, error) {
	u, err := url.Parse(pu)
	if err != nil || u.Host == "" {
		return nil, err
	}
	base := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 15 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "socks5", "socks5h", "socks":
		if scheme == "socks" {
			u.Scheme = "socks5"
		}
		dialer, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, err
		}
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			base.DialContext = cd.DialContext
		} else {
			base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			}
		}
		// SOCKS is dial-time; do not also set http.Proxy (HTTP CONNECT).
		base.Proxy = nil
		return base, nil
	default:
		// http / https / empty-with-host treated as HTTP proxy.
		if u.Scheme == "" {
			u.Scheme = "http"
		}
		base.Proxy = http.ProxyURL(u)
		return base, nil
	}
}
