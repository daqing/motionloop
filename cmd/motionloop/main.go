package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/config"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/memory"
	"github.com/daqing/motionloop/profile"
	"github.com/daqing/motionloop/profile/coding"
	promptpkg "github.com/daqing/motionloop/prompt"
	"github.com/daqing/motionloop/session"
	"github.com/daqing/motionloop/skills"
	"github.com/daqing/motionloop/tools"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "trust", "untrust":
			if err := runTrust(os.Args[1] == "trust"); err != nil {
				fmt.Fprintln(os.Stderr, "motionloop:", err)
				os.Exit(1)
			}
			return
		case "session":
			if err := runSessionCommand(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "motionloop:", err)
				os.Exit(1)
			}
			return
		}
	}

	promptFlag := flag.String("p", "", "one-shot prompt (non-interactive)")
	providerFlag := flag.String("provider", "", "provider id (settings default: openai)")
	modelFlag := flag.String("model", "", "model id (defaults to the provider's first catalog entry)")
	profileFlag := flag.String("profile", "", "agent profile (settings default: coding)")
	systemPromptPath := flag.String("system-prompt", "", "replace the profile's system prompt with this file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("motionloop " + version)
		return
	}
	if *promptFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: motionloop -p <prompt> [--provider id] [--model id] [--profile name] [--system-prompt file]")
		fmt.Fprintln(os.Stderr, "       motionloop session ls | show <id> | fork <id> | resume <id> -p <prompt>")
		fmt.Fprintln(os.Stderr, "       motionloop trust | untrust")
		fmt.Fprintln(os.Stderr, "       motionloop --version")
		os.Exit(2)
	}

	if err := run(context.Background(), *providerFlag, *modelFlag, *profileFlag, *systemPromptPath, *promptFlag); err != nil {
		fmt.Fprintln(os.Stderr, "motionloop:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, providerFlag, modelFlag, profileFlag, systemPromptPath, prompt string) error {
	app, err := prepare(providerFlag, modelFlag, profileFlag)
	if err != nil {
		return err
	}

	mgr := session.NewManager(sessionsRoot(app.home))
	sess, err := mgr.Create(app.cwd)
	if err != nil {
		return err
	}
	defer sess.Close()

	options, err := app.buildOptions(systemPromptPath, nil, sess)
	if err != nil {
		return err
	}
	a := agent.New(app.provider, app.model, options...)
	rec := session.NewRecorder(sess)
	a.Subscribe(rec.Handle)
	a.Subscribe(renderEvent)
	if err := a.Prompt(ctx, prompt); err != nil {
		return err
	}
	if err := rec.Err(); err != nil {
		return fmt.Errorf("session recording: %w", err)
	}
	return nil
}

func runTrust(trust bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	store, err := config.OpenTrustStore(filepath.Join(home, ".motionloop", "trust.json"))
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if trust {
		if err := store.Trust(cwd); err != nil {
			return err
		}
		fmt.Printf("trusted %s (project-level config and prompts will load)\n", cwd)
		return nil
	}
	if err := store.Untrust(cwd); err != nil {
		return err
	}
	fmt.Printf("untrusted %s\n", cwd)
	return nil
}

// app carries the resolved runtime pieces shared by every entry point.
type app struct {
	home       string
	cwd        string
	trusted    bool
	settings   config.Settings
	ws         *tools.Workspace
	prof       profile.Profile
	provider   llm.Provider
	model      llm.Model
	streamOpts llm.StreamOptions
}

// prepare resolves configuration, provider, model, and profile. Empty
// flags fall back to settings and defaults.
func prepare(providerFlag, modelFlag, profileFlag string) (*app, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	trustStore, err := config.OpenTrustStore(filepath.Join(home, ".motionloop", "trust.json"))
	if err != nil {
		return nil, err
	}
	trusted := trustStore.Status(cwd)
	if !trusted {
		fmt.Fprintln(os.Stderr, "motionloop: project not trusted; project-level config and prompts skipped (run: motionloop trust)")
	}
	settings, err := config.Load(config.Sources{
		GlobalDir:  filepath.Join(home, ".motionloop"),
		ProjectDir: filepath.Join(cwd, ".motionloop"),
		Trusted:    trusted,
	})
	if err != nil {
		return nil, err
	}
	providerID := firstNonEmpty(providerFlag, settings.Provider)
	modelID := firstNonEmpty(modelFlag, settings.Model)
	profileName := firstNonEmpty(profileFlag, settings.Profile)

	provider, err := llm.Resolve(providerID)
	if err != nil {
		return nil, err
	}
	catalog, customEnvs, err := config.LoadModels(filepath.Join(home, ".motionloop", "models.json"))
	if err != nil {
		return nil, err
	}
	model, err := config.ResolveModel(provider, modelID, catalog)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, "motionloop:", model.String())

	ws, err := tools.NewWorkspace(cwd)
	if err != nil {
		return nil, err
	}
	prof, err := lookupProfile(profileName)
	if err != nil {
		return nil, err
	}
	return &app{
		home:       home,
		cwd:        cwd,
		trusted:    trusted,
		settings:   settings,
		ws:         ws,
		prof:       prof,
		provider:   provider,
		model:      model,
		streamOpts: llm.StreamOptions{APIKey: config.ResolveAPIKey(providerID, customEnvs[providerID])},
	}, nil
}

// buildOptions assembles the agent options: system message (or the
// --system-prompt replacement), tool loadout, skills and memory sections,
// an optional replayed transcript seed, recording-driven compaction, and
// base stream options.
func (a *app) buildOptions(systemPromptPath string, seed []llm.Message, sess *session.Session) ([]agent.Option, error) {
	var options []agent.Option
	if len(seed) > 0 {
		options = append(options, agent.WithMessages(seed...))
	}

	var extraTools []agent.Tool
	if systemPromptPath != "" {
		content, err := os.ReadFile(systemPromptPath)
		if err != nil {
			return nil, fmt.Errorf("read system prompt: %w", err)
		}
		options = append(options, agent.WithSystemPrompt(string(content)))
	} else {
		sections := a.prof.Prompt(promptpkg.DetectEnvironment(a.cwd))
		if err := applyPromptOverrides(sections, a.prof.Name, a.cwd, a.trusted); err != nil {
			return nil, err
		}

		skillList, warns, err := skills.Discover(skills.DefaultDirs(a.home, a.cwd, a.trusted))
		if err != nil {
			return nil, err
		}
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, "motionloop: skill:", w)
		}
		if idx := skills.Index(skillList); idx != "" {
			sections.Set(promptpkg.SectionSkills, idx)
		}
		extraTools = append(extraTools, &tools.SkillsLoad{List: skillList})

		memStore := memory.NewStore(filepath.Join(a.home, ".motionloop", "memory"))
		memSection := memory.UsageSection()
		slug := session.WorkspaceSlug(a.cwd)
		if idx := memStore.Index(slug); idx != "" {
			memSection += "\n\n" + idx
		}
		sections.Set(promptpkg.SectionMemory, memSection)
		extraTools = append(extraTools, &tools.MemorySave{Store: memStore, Slug: slug})

		options = append(options, agent.WithSystemMessage(sections.ToMessage()))
	}
	options = append(options,
		agent.WithTools(append(a.prof.Tools(a.ws), extraTools...)...),
		agent.WithThinkingLevel(a.prof.ThinkingLevel),
		agent.WithStreamOptions(a.streamOpts),
	)

	if sess != nil && a.settings.CompactionThreshold >= 0 {
		compactor := &session.Compactor{
			Sess:      sess,
			Provider:  a.provider,
			Model:     a.model,
			Opts:      llm.StreamOptions{APIKey: a.streamOpts.APIKey, BaseURL: a.streamOpts.BaseURL},
			Threshold: a.settings.CompactionThreshold,
		}
		options = append(options, agent.WithTransformContext(compactor.Transform))
	}
	return options, nil
}

func lookupProfile(name string) (profile.Profile, error) {
	switch name {
	case "", "coding":
		return coding.Profile, nil
	default:
		return profile.Profile{}, fmt.Errorf("unknown profile %q (available: coding)", name)
	}
}

// applyPromptOverrides layers the prompt override chain onto the profile's
// sections: built-in content, then ~/.motionloop/prompts/<profile>/, then
// the trusted project's .motionloop/prompts/<profile>/.
func applyPromptOverrides(sections *promptpkg.Sections, profileName, cwd string, trusted bool) error {
	home, err := os.UserHomeDir()
	if err == nil {
		if err := sections.ApplyOverrideDir(filepath.Join(home, ".motionloop", "prompts", profileName)); err != nil {
			return err
		}
	}
	if !trusted {
		return nil
	}
	return sections.ApplyOverrideDir(filepath.Join(cwd, ".motionloop", "prompts", profileName))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// renderEvent prints assistant text to stdout and tool activity to stderr —
// the line-oriented contract until a richer UI arrives.
func renderEvent(ev agent.Event) {
	switch e := ev.(type) {
	case agent.MessageUpdate:
		if d, ok := e.Delta.(llm.TextDelta); ok {
			fmt.Print(d.Delta)
		}
	case agent.MessageEnd:
		if e.Message.Role == llm.RoleAssistant {
			if s := assistantText(e.Message); s != "" && !strings.HasSuffix(s, "\n") {
				fmt.Println()
			}
		}
	case agent.ToolExecutionStart:
		fmt.Fprintf(os.Stderr, "[tool] %s %s\n", e.ToolName, string(e.Arguments))
	case agent.ToolExecutionEnd:
		if e.IsError {
			fmt.Fprintf(os.Stderr, "[tool] %s: error\n", e.ToolName)
		}
	case agent.AgentEnd:
		if last := lastAssistant(e.Messages); last != nil && last.Usage != nil {
			fmt.Fprintf(os.Stderr, "[usage] in=%d out=%d total=%d\n",
				last.Usage.Input, last.Usage.Output, last.Usage.TotalTokens)
		}
	}
}

func assistantText(m llm.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if t, ok := b.(llm.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

func lastAssistant(msgs []llm.Message) *llm.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleAssistant {
			return &msgs[i]
		}
	}
	return nil
}
