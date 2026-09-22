package tools

// Mutate serializes file mutations within one workspace — a second line of
// defense behind the agent loop's sequential batch mode, and the guard for
// callers invoking tool Execute directly.
func (w *Workspace) Mutate(fn func() error) error {
	w.mutMu.Lock()
	defer w.mutMu.Unlock()
	return fn()
}
