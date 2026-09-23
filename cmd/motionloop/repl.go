package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/config"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/session"
)

// runREPL drives the interactive loop: line input with slash commands,
// streaming output, steering while a run is active, and interrupt
// semantics (one Ctrl+C cancels the current run, a second exits).
func runREPL(ctx context.Context, providerFlag, modelFlag, profileFlag string) error {
	app, err := prepare(providerFlag, modelFlag, profileFlag)
	if err != nil {
		return err
	}

	mgr := session.NewManager(sessionsRoot(app.home))
	sess, err := mgr.Create(app.cwd)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	options, compactor, err := app.buildOptions("", nil, sess)
	if err != nil {
		return err
	}
	a := agent.New(app.provider, app.model, options...)
	rec := session.NewRecorder(sess)
	a.Subscribe(rec.Handle)
	a.Subscribe(renderEvent)

	fmt.Printf("motionloop %s — %s (session %s)\n", version, app.model.String(), shortID(sess.Header().ID))
	fmt.Println("Type a prompt, or /help for commands. Ctrl+C interrupts a run; twice exits.")

	lines := make(chan string)
	go readLines(lines)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	consecutiveInterrupts := 0
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				fmt.Println()
				return nil
			}
			consecutiveInterrupts = 0
			trim := strings.TrimSpace(line)
			switch {
			case trim == "":
			case trim == "/exit" || trim == "/quit":
				return nil
			case trim == "/help":
				printREPLHelp()
			case trim == "/fork":
				child, err := sess.Fork()
				if err != nil {
					fmt.Fprintln(os.Stderr, "motionloop:", err)
					continue
				}
				fmt.Printf("forked to session %s\n", shortID(child.Header().ID))
				_ = child.Close()
			case trim == "/compact":
				handleCompact(a, compactor)
			case strings.HasPrefix(trim, "/model"):
				if err := handleModel(app, a, sess, strings.TrimSpace(strings.TrimPrefix(trim, "/model"))); err != nil {
					fmt.Fprintln(os.Stderr, "motionloop:", err)
				}
			default:
				runCtx, cancelRun := context.WithCancel(ctx)
				if err := runREPLPrompt(a, runCtx, cancelRun, lines, sig, trim); err != nil {
					if errors.Is(err, errExitREPL) {
						return nil
					}
					return err
				}
			}
		case <-sig:
			consecutiveInterrupts++
			if consecutiveInterrupts >= 2 {
				fmt.Println()
				return nil
			}
			fmt.Fprintln(os.Stderr, "(interrupt — press Ctrl+C again to exit)")
		}
	}
}

// runREPLPrompt runs one prompt to completion while accepting steering
// input and interrupts.
func runREPLPrompt(a *agent.Agent, ctx context.Context, cancel context.CancelFunc, lines <-chan string, sig <-chan os.Signal, prompt string) error {
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Prompt(ctx, prompt) }()
	for {
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "motionloop:", err)
			}
			return nil
		case line := <-lines:
			trim := strings.TrimSpace(line)
			if trim == "/exit" || trim == "/quit" {
				cancel()
				return errExitREPL
			}
			if trim != "" {
				a.Steer(trim)
				fmt.Fprintln(os.Stderr, "(steered)")
			}
		case <-sig:
			cancel()
			fmt.Fprintln(os.Stderr, "(interrupted)")
			<-done // wait for the aborted run to settle
			return nil
		}
	}
}

// errExitREPL unwinds the loop from inside a run without printing errors.
var errExitREPL = errors.New("repl exit")

func readLines(out chan<- string) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for {
		fmt.Print("» ")
		if !scanner.Scan() {
			close(out)
			return
		}
		out <- scanner.Text()
	}
}

func printREPLHelp() {
	fmt.Println("/help              show commands")
	fmt.Println("/model [p/m|model] switch model (e.g. glm/glm-4.6)")
	fmt.Println("/compact           compact the transcript now")
	fmt.Println("/fork              fork the session at this point")
	fmt.Println("/exit              leave the REPL")
	fmt.Println("Anything else is a prompt; typing during a run steers it.")
}

func handleCompact(a *agent.Agent, compactor *session.Compactor) {
	if compactor == nil {
		fmt.Fprintln(os.Stderr, "compaction disabled (compactionThreshold < 0)")
		return
	}
	view := compactor.Force(context.Background(), a.Messages())
	fmt.Printf("compacted: request view now %d messages\n", len(view))
}

func handleModel(app *app, a *agent.Agent, sess *session.Session, arg string) error {
	providerID := app.providerID
	modelID := arg
	if i := strings.Index(arg, "/"); i >= 0 {
		providerID, modelID = arg[:i], arg[i+1:]
	}
	if modelID == "" {
		fmt.Printf("current: %s\n", app.model.String())
		return nil
	}
	provider, err := llm.Resolve(providerID)
	if err != nil {
		return err
	}
	model, err := config.ResolveModel(provider, modelID, app.catalog)
	if err != nil {
		return err
	}
	if err := a.SetProvider(provider); err != nil {
		return err
	}
	if err := a.SetModel(model); err != nil {
		return err
	}
	app.provider, app.model, app.providerID = provider, model, providerID
	app.streamOpts.Credentials = llm.Credentials{APIKey: config.ResolveAPIKey(providerID, app.customEnvs[providerID])}
	if err := a.SetStreamOptions(app.streamOpts); err != nil {
		return err
	}
	if err := sess.AppendModelChange(providerID, model.ModelID); err != nil {
		return err
	}
	fmt.Printf("switched to %s\n", model.String())
	return nil
}
