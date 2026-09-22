package llm

// Catalog merges model entries from several sources; later entries
// override earlier ones by provider/model id.
type Catalog struct {
	models []Model
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog { return &Catalog{} }

// Add merges one batch of entries into the catalog.
func (c *Catalog) Add(models []Model) {
	for _, m := range models {
		replaced := false
		for i := range c.models {
			if c.models[i].ProviderID == m.ProviderID && c.models[i].ModelID == m.ModelID {
				c.models[i] = m
				replaced = true
				break
			}
		}
		if !replaced {
			c.models = append(c.models, m)
		}
	}
}

// Resolve looks up one model entry.
func (c *Catalog) Resolve(providerID, modelID string) (Model, bool) {
	for _, m := range c.models {
		if m.ProviderID == providerID && m.ModelID == modelID {
			return m, true
		}
	}
	return Model{}, false
}

// Models returns all entries in insertion order.
func (c *Catalog) Models() []Model { return c.models }

// ModelsOf returns a provider's declared catalog entries when it
// implements the optional modelCatalog interface.
func ModelsOf(p Provider) []Model {
	type modelCatalog interface{ Models() []Model }
	if mc, ok := p.(modelCatalog); ok {
		return mc.Models()
	}
	return nil
}
