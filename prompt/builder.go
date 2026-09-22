package prompt

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// GitInfo summarizes the workspace repository state; nil when the
// directory is not a git repository.
type GitInfo struct {
	Branch string
	Dirty  bool
}

// Environment carries the dynamic facts a prompt renders.
type Environment struct {
	Cwd      string
	Platform string
	Date     string
	Git      *GitInfo
}

// DetectEnvironment collects environment facts about dir. Git facts are
// best-effort: no git binary or no repository leaves Git nil.
func DetectEnvironment(dir string) Environment {
	env := Environment{
		Cwd:      dir,
		Platform: runtime.GOOS + " (" + runtime.GOARCH + ")",
		Date:     time.Now().Format("2006-01-02"),
	}
	env.Git = detectGit(dir)
	return env
}

func detectGit(dir string) *GitInfo {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	branch, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return nil
	}
	status, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return nil
	}
	return &GitInfo{
		Branch: strings.TrimSpace(string(branch)),
		Dirty:  len(strings.TrimSpace(string(status))) > 0,
	}
}

// EnvironmentSection renders the environment facts as the environment
// section body.
func EnvironmentSection(env Environment) string {
	var sb strings.Builder
	sb.WriteString("# Environment\n\n")
	fmt.Fprintf(&sb, "- Working directory: %s\n", env.Cwd)
	fmt.Fprintf(&sb, "- Platform: %s\n", env.Platform)
	fmt.Fprintf(&sb, "- Date: %s\n", env.Date)
	switch {
	case env.Git == nil:
		sb.WriteString("- Git: not a repository\n")
	case env.Git.Dirty:
		fmt.Fprintf(&sb, "- Git: branch %s, uncommitted changes present\n", env.Git.Branch)
	default:
		fmt.Fprintf(&sb, "- Git: branch %s, clean working tree\n", env.Git.Branch)
	}
	return strings.TrimRight(sb.String(), "\n")
}
