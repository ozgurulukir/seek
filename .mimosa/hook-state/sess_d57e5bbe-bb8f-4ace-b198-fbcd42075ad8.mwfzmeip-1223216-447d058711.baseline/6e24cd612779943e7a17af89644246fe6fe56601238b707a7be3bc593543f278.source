package embed

import (
	"net/http"
	"testing"
)

func TestOfflineClientsDisableProxyAndRejectRedirects(t *testing.T) {
	clients := []*http.Client{
		newClient("http://127.0.0.1", "key", "model", 3, TaskPrefix{}, true).http,
		newVLClient("key", "model", 3, "http://127.0.0.1", TaskPrefix{}, true).http,
		newRerankClient("http://127.0.0.1", "key", "model", true).http,
	}
	for _, client := range clients {
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil {
			t.Fatalf("offline client transport proxy = %v, want nil", transport)
		}
		if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
			t.Fatalf("offline client redirect error = %v, want %v", err, http.ErrUseLastResponse)
		}
	}
}

func TestRemoteClientsKeepDefaultHTTPBehavior(t *testing.T) {
	clients := []*http.Client{
		newClient("https://example.com", "key", "model", 3, TaskPrefix{}, false).http,
		newVLClient("key", "model", 3, "https://example.com", TaskPrefix{}, false).http,
		newRerankClient("https://example.com", "key", "model", false).http,
	}
	for _, client := range clients {
		if client.CheckRedirect != nil {
			t.Fatal("remote client unexpectedly rejects redirects")
		}
		if client.Transport != nil {
			t.Fatal("remote client unexpectedly overrides the default transport")
		}
	}
}
