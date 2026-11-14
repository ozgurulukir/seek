package embed

import (
	"strings"
	"testing"
)

func TestOfflineClientRefusesNetwork(t *testing.T) {
	c := NewOfflineClient("test-model")
	if _, err := c.EmbedDocuments([]string{"secret note text"}); err == nil {
		t.Fatal("offline client sent a request; want refusal")
	} else if !strings.Contains(err.Error(), "offline_only") {
		t.Errorf("error = %v, want offline_only mention", err)
	}
	if _, err := c.EmbedQuery("secret query"); err == nil {
		t.Error("EmbedQuery also must be refused offline")
	}
	if _, err := c.BatchEmbed([]string{"a", "b"}, 2); err == nil {
		t.Error("BatchEmbed also must be refused offline")
	}
}
