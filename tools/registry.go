package tools

import "github.com/daqing/motionloop/agent"

// Coding returns the full built-in tool loadout for coding agents.
func Coding(ws *Workspace) []agent.Tool {
	return []agent.Tool{
		Bash{},
		Read{WS: ws},
		Write{WS: ws},
		Edit{WS: ws},
		Grep{WS: ws},
		Glob{WS: ws},
		Ls{WS: ws},
	}
}

// Minimal returns the minimal loadout: bash, read, write.
func Minimal(ws *Workspace) []agent.Tool {
	return []agent.Tool{
		Bash{},
		Read{WS: ws},
		Write{WS: ws},
	}
}
