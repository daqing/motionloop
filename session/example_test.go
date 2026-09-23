package session_test

import (
	"context"
	"fmt"

	"github.com/daqing/motionloop/agent"
	"github.com/daqing/motionloop/llm"
	"github.com/daqing/motionloop/session"
)

// ExampleManager_recordAndReplay shows the persistence round trip: run an
// agent with a recorder, close the file, and rebuild the transcript from
// disk alone. (Compile-only example; the same flow is executed in
// resume_test.go.)
func ExampleManager_recordAndReplay() {
	root, _ := session.DefaultRoot() // typically ~/.motionloop/sessions
	mgr := session.NewManager(root)
	sess, err := mgr.Create("/project")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fake := agent.NewFakeProvider(agent.FakeTextEvents("hello from the run"))
	a := agent.New(fake, llm.Model{ProviderID: "fake", ModelID: "demo"},
		agent.WithSystemPrompt("be terse"),
	)
	rec := session.NewRecorder(sess)
	a.Subscribe(rec.Handle)
	if err := a.Prompt(context.Background(), "hi"); err != nil {
		fmt.Println("error:", err)
	}
	sess.Close()

	_, state, err := mgr.Load(sess.Path())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, m := range state.Messages {
		fmt.Println(m.Role)
	}
}
