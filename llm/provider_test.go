package llm

import (
	"context"
	"strings"
	"testing"
)

type fakeProvider struct{ id string }

func (f fakeProvider) ID() string { return f.id }

func (f fakeProvider) Stream(context.Context, Model, []Message, StreamOptions) (<-chan StreamEvent, error) {
	return nil, nil
}

func (f fakeProvider) Capabilities(model Model) Capabilities { return model.Capabilities }

func TestRegisterResolve(t *testing.T) {
	Register(fakeProvider{id: "fake-reg"})

	p, err := Resolve("fake-reg")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.ID() != "fake-reg" {
		t.Fatalf("got ID %q, want %q", p.ID(), "fake-reg")
	}

	_, err = Resolve("no-such-provider")
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("want unknown-provider error, got %v", err)
	}

	ids := ProviderIDs()
	found := false
	for _, id := range ids {
		if id == "fake-reg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ProviderIDs() = %v, want fake-reg registered", ids)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("want panic on duplicate registration")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "duplicate") {
			t.Fatalf("unexpected panic value: %v", r)
		}
	}()
	Register(fakeProvider{id: "fake-dup"})
	Register(fakeProvider{id: "fake-dup"})
}
