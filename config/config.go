// Package config resolves motionloop's layered configuration: built-in
// defaults, global and (trusted) project settings.json, MOTIONLOOP_* env
// overrides, custom models.json providers, and project trust fingerprints.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Settings is the resolved user configuration.
type Settings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Profile  string `json:"profile"`
	// CompactionThreshold is the estimated-token trigger for transcript
	// compaction: 0 means the default, negative disables compaction.
	CompactionThreshold int `json:"compactionThreshold"`
}

// Defaults returns the built-in settings.
func Defaults() Settings {
	return Settings{Provider: "openai", Profile: "coding"}
}

// Sources names the configuration locations Load consults.
type Sources struct {
	GlobalDir  string // ~/.motionloop
	ProjectDir string // <cwd>/.motionloop; only read when Trusted
	Trusted    bool
	// Env resolves environment variables; nil means os.Getenv.
	Env func(string) string
}

// Load resolves settings with precedence: defaults → global settings.json
// → trusted project settings.json → MOTIONLOOP_PROVIDER / _MODEL /
// _PROFILE env.
func Load(src Sources) (Settings, error) {
	s := Defaults()
	read := func(dir string) error {
		if dir == "" {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("config: read settings: %w", err)
		}
		var partial map[string]any
		if err := json.Unmarshal(data, &partial); err != nil {
			return fmt.Errorf("config: parse %s: %w", filepath.Join(dir, "settings.json"), err)
		}
		return applyJSON(&s, partial)
	}
	if err := read(src.GlobalDir); err != nil {
		return s, err
	}
	if src.Trusted {
		if err := read(src.ProjectDir); err != nil {
			return s, err
		}
	}
	get := src.Env
	if get == nil {
		get = os.Getenv
	}
	for key, field := range map[string]*string{
		"MOTIONLOOP_PROVIDER": &s.Provider,
		"MOTIONLOOP_MODEL":    &s.Model,
		"MOTIONLOOP_PROFILE":  &s.Profile,
	} {
		if v := get(key); v != "" {
			*field = v
		}
	}
	return s, nil
}

func applyJSON(s *Settings, m map[string]any) error {
	for key, field := range map[string]*string{
		"provider": &s.Provider,
		"model":    &s.Model,
		"profile":  &s.Profile,
	} {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("config: setting %q must be a string", key)
		}
		if str != "" {
			*field = str
		}
	}
	if v, ok := m["compactionThreshold"]; ok && v != nil {
		n, ok := v.(float64)
		if !ok || n != float64(int(n)) {
			return fmt.Errorf("config: setting %q must be an integer", "compactionThreshold")
		}
		s.CompactionThreshold = int(n)
	}
	return nil
}
