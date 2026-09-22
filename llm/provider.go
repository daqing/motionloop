package llm

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Provider is the extension point for LLM backends. Implementations
// register themselves via Register, typically from an init function in a
// subpackage that the binary blank-imports.
type Provider interface {
	// ID returns the provider identifier used in Model.ProviderID and
	// user-facing configuration.
	ID() string

	// Stream performs one streaming completion request. Apart from context
	// cancellation, Stream must not return an error: request, protocol,
	// and transport failures are delivered as Stop{Reason:
	// StopReasonError} on the event channel, and every well-formed stream
	// ends with exactly one Stop event.
	Stream(ctx context.Context, model Model, messages []Message, opts StreamOptions) (<-chan StreamEvent, error)

	// Capabilities reports what the provider supports for one model.
	Capabilities(model Model) Capabilities
}

var (
	providersMu sync.RWMutex
	providers   = map[string]Provider{}
)

// Register adds a provider to the global registry. Registering a duplicate
// ID panics, surfacing wiring bugs at startup.
func Register(p Provider) {
	providersMu.Lock()
	defer providersMu.Unlock()
	id := p.ID()
	if _, dup := providers[id]; dup {
		panic("llm: duplicate provider registration: " + id)
	}
	providers[id] = p
}

// Resolve looks up a registered provider by ID.
func Resolve(providerID string) (Provider, error) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	p, ok := providers[providerID]
	if !ok {
		return nil, fmt.Errorf("llm: unknown provider %q", providerID)
	}
	return p, nil
}

// ProviderIDs returns every registered provider ID, sorted.
func ProviderIDs() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
