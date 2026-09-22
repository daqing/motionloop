package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/daqing/motionloop/llm"
)

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPrecedence(t *testing.T) {
	global := t.TempDir()
	project := t.TempDir()

	cases := []struct {
		name    string
		global  string // settings.json content, "" = absent
		project string
		trusted bool
		env     map[string]string
		want    Settings
	}{
		{
			name: "defaults",
			want: Settings{Provider: "openai", Profile: "coding"},
		},
		{
			name:   "global overrides defaults",
			global: `{"provider":"glm","model":"glm-4.6"}`,
			want:   Settings{Provider: "glm", Model: "glm-4.6", Profile: "coding"},
		},
		{
			name:    "project overrides global when trusted",
			global:  `{"provider":"glm"}`,
			project: `{"provider":"deepseek","model":"deepseek-chat"}`,
			trusted: true,
			want:    Settings{Provider: "deepseek", Model: "deepseek-chat", Profile: "coding"},
		},
		{
			name:    "project ignored when untrusted",
			global:  `{"provider":"glm"}`,
			project: `{"provider":"deepseek"}`,
			trusted: false,
			want:    Settings{Provider: "glm", Profile: "coding"},
		},
		{
			name:   "env beats everything",
			global: `{"provider":"glm"}`,
			env:    map[string]string{"MOTIONLOOP_PROVIDER": "anthropic", "MOTIONLOOP_PROFILE": "custom"},
			want:   Settings{Provider: "anthropic", Profile: "custom"},
		},
		{
			name:   "malformed settings rejected",
			global: `{`,
			want:   Settings{Provider: "openai", Profile: "coding"},
		},
	}
	for _, tc := range cases {
		if tc.name == "malformed settings rejected" {
			continue // handled below with error assertion
		}
		t.Run(tc.name, func(t *testing.T) {
			if tc.global != "" {
				writeJSON(t, filepath.Join(global, "settings.json"), tc.global)
			} else {
				os.Remove(filepath.Join(global, "settings.json"))
			}
			if tc.project != "" {
				writeJSON(t, filepath.Join(project, "settings.json"), tc.project)
			}
			got, err := Load(Sources{
				GlobalDir:  global,
				ProjectDir: project,
				Trusted:    tc.trusted,
				Env:        func(k string) string { return tc.env[k] },
			})
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got != tc.want {
				t.Fatalf("settings = %+v, want %+v", got, tc.want)
			}
		})
	}

	t.Run("malformed settings rejected", func(t *testing.T) {
		writeJSON(t, filepath.Join(global, "settings.json"), `{`)
		if _, err := Load(Sources{GlobalDir: global}); err == nil {
			t.Fatal("want parse error")
		}
	})
}

func TestLoadModels(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file is empty", func(t *testing.T) {
		catalog, envs, err := LoadModels(filepath.Join(dir, "absent.json"))
		if err != nil || catalog == nil || len(envs) != 0 {
			t.Fatalf("catalog = %v, envs = %v, err = %v", catalog, envs, err)
		}
	})

	path := filepath.Join(dir, "models.json")
	writeJSON(t, path, `{
		"providers": [
			{
				"id": "myllm",
				"baseUrl": "http://localhost:11434/v1",
				"apiKeyEnv": "MYLLM_KEY",
				"models": [
					{"id": "llama-x", "contextWindow": 131072, "images": true}
				]
			}
		]
	}`)
	catalog, envs, err := LoadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := catalog.Resolve("myllm", "llama-x")
	if !ok || m.ContextWindow != 131072 || !m.Capabilities.Images {
		t.Fatalf("entry = %+v", m)
	}
	if envs["myllm"] != "MYLLM_KEY" {
		t.Fatalf("envs = %v", envs)
	}

	// registered and resolvable through the registry
	p, err := llm.Resolve("myllm")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "myllm" {
		t.Fatalf("provider = %q", p.ID())
	}

	t.Run("duplicate provider id rejected", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("want panic on duplicate registration")
			}
		}()
		_, _, _ = LoadModels(path)
	})
}

func TestResolveAPIKeyOrder(t *testing.T) {
	t.Setenv("MYLLM_KEY", "custom")
	t.Setenv("OPENAI_API_KEY", "vendor")
	t.Setenv("MOTIONLOOP_API_KEY", "fallback")

	if got := ResolveAPIKey("myllm", "MYLLM_KEY"); got != "custom" {
		t.Fatalf("custom env lost: %q", got)
	}
	if got := ResolveAPIKey("openai", ""); got != "vendor" {
		t.Fatalf("vendor env lost: %q", got)
	}
	if got := ResolveAPIKey("unknown", ""); got != "fallback" {
		t.Fatalf("fallback lost: %q", got)
	}
	t.Setenv("OPENAI_API_KEY", "")
	if got := ResolveAPIKey("openai", ""); got != "fallback" {
		t.Fatalf("fallback after empty vendor env: %q", got)
	}
}

func TestTrustStoreLifecycle(t *testing.T) {
	file := filepath.Join(t.TempDir(), "trust.json")
	store, err := OpenTrustStore(file)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if store.Status(repo) {
		t.Fatal("fresh project must be untrusted")
	}
	if err := store.Trust(repo); err != nil {
		t.Fatal(err)
	}
	if !store.Status(repo) {
		t.Fatal("trusted project reported untrusted")
	}

	// persisted: a fresh store sees it
	reopened, err := OpenTrustStore(file)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Status(repo) {
		t.Fatal("trust did not persist")
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed; fingerprint-change path untested")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// the fingerprint hashes path + remote origin URL: gaining a remote,
	// or changing it, revokes trust until the user trusts again
	run("init", "-q")
	if !reopened.Status(repo) {
		t.Fatal("git init without remote must not change the fingerprint")
	}
	run("remote", "add", "origin", "https://example.com/x.git")
	if reopened.Status(repo) {
		t.Fatal("gaining a remote must revoke trust")
	}
	if err := reopened.Trust(repo); err != nil {
		t.Fatal(err)
	}
	if !reopened.Status(repo) {
		t.Fatal("re-trust after fingerprint change failed")
	}
	run("remote", "set-url", "origin", "https://example.com/y.git")
	if reopened.Status(repo) {
		t.Fatal("changed remote must revoke trust")
	}
	if err := reopened.Untrust(repo); err != nil {
		t.Fatal(err)
	}
	if reopened.Status(repo) {
		t.Fatal("untrust did not stick")
	}
}
