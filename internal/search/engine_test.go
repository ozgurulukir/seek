package search

import (
	"testing"

	"github.com/ozgurulukir/seek/internal/embed"
	"github.com/ozgurulukir/seek/internal/store"
)

func TestNewEngine(t *testing.T) {
	s := &store.Store{}
	ec := &embed.Client{}

	engine := NewEngine(s, ec)
	if engine == nil {
		t.Fatal("expected engine to not be nil")
	}
	if engine.store != s {
		t.Errorf("expected store to be set")
	}
	if engine.embedClient != ec {
		t.Errorf("expected embedClient to be set")
	}
	if engine.vlClient != nil {
		t.Errorf("expected vlClient to be nil")
	}
}

func TestNewEngineWithVL(t *testing.T) {
	s := &store.Store{}
	ec := &embed.Client{}
	vlc := &embed.VLClient{}

	engine := NewEngineWithVL(s, ec, vlc)
	if engine == nil {
		t.Fatal("expected engine to not be nil")
	}
	if engine.store != s {
		t.Errorf("expected store to be set")
	}
	if engine.embedClient != ec {
		t.Errorf("expected embedClient to be set")
	}
	if engine.vlClient != vlc {
		t.Errorf("expected vlClient to be set")
	}
}
