package agent

import (
	"context"
	"testing"

	"github.com/daqing/motionloop/llm"
)

func TestWithAPIKeyAndBaseURL(t *testing.T) {
	fake := NewFakeProvider(FakeTextEvents("ok"))
	a := New(fake, llm.Model{ProviderID: "fake", ModelID: "test"},
		WithStreamOptions(llm.StreamOptions{MaxTokens: 64}),
		WithAPIKey("sk-test"),
		WithBaseURL("http://proxy.example"),
	)
	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	opts := fake.Calls()[0].Options
	if opts.Credentials.APIKey != "sk-test" || opts.Credentials.BaseURL != "http://proxy.example" {
		t.Fatalf("credentials = %+v", opts.Credentials)
	}
	if opts.MaxTokens != 64 {
		t.Fatalf("stream shaping lost: %+v", opts)
	}
}
