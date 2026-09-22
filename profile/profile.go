// Package profile bundles prompt assembly, a tool preset, and default
// request settings into a deployable agent persona. coding is the built-in
// profile; libraries define their own.
package profile

import (
	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/prompt"
	"github.com/daqing/motionloop/tools"
)

// Profile is one deployable agent configuration.
type Profile struct {
	Name string
	// Prompt builds the base sections for one environment.
	Prompt func(env prompt.Environment) *prompt.Sections
	// Tools builds the tool loadout for one workspace.
	Tools func(ws *tools.Workspace) []agent.Tool
	// ThinkingLevel is the default reasoning tier.
	ThinkingLevel llm.ThinkingLevel
}

// AgentOptions translates the profile into agent options: system message,
// tool loadout, thinking level, and provider options.
func (p Profile) AgentOptions(ws *tools.Workspace, env prompt.Environment, streamOpts llm.StreamOptions) []agent.Option {
	return []agent.Option{
		agent.WithSystemMessage(p.Prompt(env).ToMessage()),
		agent.WithTools(p.Tools(ws)...),
		agent.WithThinkingLevel(p.ThinkingLevel),
		agent.WithStreamOptions(streamOpts),
	}
}
