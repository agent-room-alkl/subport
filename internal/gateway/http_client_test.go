package gateway

import (
	"net/http"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
)

func TestTransportForProxyURLSOCKS5(t *testing.T) {
	tr, err := transportForProxyURL("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Proxy != nil {
		t.Fatal("SOCKS5 transport must not set HTTP Proxy")
	}
	if tr.DialContext == nil {
		t.Fatal("SOCKS5 transport needs DialContext")
	}
}

func TestTransportForProxyURLHTTP(t *testing.T) {
	tr, err := transportForProxyURL("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Proxy == nil {
		t.Fatal("HTTP proxy transport must set Proxy")
	}
}

func TestHTTPClientForUsesRegisteredSOCKS(t *testing.T) {
	SetProxyURL("p-socks", "socks5://127.0.0.1:1080")
	defer ClearProxyURL("p-socks")
	c := HTTPClientFor(model.Account{ProxyID: "p-socks"})
	if c == nil || c == upstreamClient {
		t.Fatal("expected proxied client, got shared upstreamClient")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr == nil {
		t.Fatalf("Transport type = %T", c.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("SOCKS client must dial via SOCKS, not HTTP CONNECT Proxy")
	}
}

func TestHTTPClientForMissingProxyFallsBack(t *testing.T) {
	c := HTTPClientFor(model.Account{ProxyID: "missing"})
	if c != upstreamClient {
		t.Fatal("missing proxy registration should fall back to upstreamClient")
	}
}

func TestNormalizeProxyURLSOCKSType(t *testing.T) {
	got := NormalizeProxyURL("socks5", "127.0.0.1:1080")
	if got != "socks5://127.0.0.1:1080" {
		t.Fatalf("got %q", got)
	}
	got = NormalizeProxyURL("http", "127.0.0.1:8080")
	if got != "http://127.0.0.1:8080" {
		t.Fatalf("got %q", got)
	}
	got = NormalizeProxyURL("socks5", "socks5://already:1080")
	if got != "socks5://already:1080" {
		t.Fatalf("got %q", got)
	}
}