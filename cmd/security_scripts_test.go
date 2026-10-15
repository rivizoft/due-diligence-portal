package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type workflowStep struct {
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

func branchStep(t *testing.T) workflowStep {
	t.Helper()
	data, err := os.ReadFile("../.github/workflows/build-and-push.yml")
	require.NoError(t, err)
	var workflow struct {
		Jobs map[string]struct {
			Steps []workflowStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &workflow))
	for _, step := range workflow.Jobs["setup"].Steps {
		if step.Name == "Extract branch name" {
			return step
		}
	}
	t.Fatal("branch extraction step missing")
	return workflowStep{}
}

func TestWorkflowBranchNamesAreData(t *testing.T) {
	for _, event := range []string{"pull_request", "push"} {
		t.Run(event, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "injected")
			branch := "audit$(touch${IFS}" + marker + ")"
			// This is a valid Git ref, not an impossible branch name.
			require.NoError(t, exec.Command("git", "check-ref-format", "--branch", branch).Run())
			runBranchStep(t, event, branch, dir)
			_, err := os.Stat(marker)
			require.True(t, os.IsNotExist(err), "branch name executed shell commands")
		})
	}
}

func TestWorkflowProducesValidImageTags(t *testing.T) {
	for _, branch := range []string{"main", "feature/s3", "Feature/UPPER", "audit$(printf${IFS}marker)", "-leading", strings.Repeat("a", 150)} {
		t.Run(branch[:min(30, len(branch))], func(t *testing.T) {
			tag := runBranchStep(t, "push", branch, t.TempDir())
			require.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`), tag)
			if branch == "main" {
				require.Equal(t, "main", tag)
			}
			if branch == "feature/s3" {
				require.Equal(t, "feature-s3", tag)
			}
		})
	}
}

func runBranchStep(t *testing.T, event, branch, dir string) string {
	t.Helper()
	step := branchStep(t)
	replace := strings.NewReplacer("${{ github.event_name }}", event, "${{ github.head_ref }}", branch, "${{ github.ref_name }}", branch)
	output := filepath.Join(dir, "github-output")
	cmd := exec.Command("bash", "-e", "-c", replace.Replace(step.Run))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "GITHUB_OUTPUT=" + output}
	for key, value := range step.Env {
		cmd.Env = append(cmd.Env, key+"="+replace.Replace(value))
	}
	log, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", log)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), "tag="))
	return strings.TrimSpace(strings.TrimPrefix(string(data), "tag="))
}

func TestStartScriptProtectsJWTSecret(t *testing.T) {
	for _, kind := range []string{"new", "existing readable", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			data, err := os.ReadFile("../start.sh")
			require.NoError(t, err)
			script := filepath.Join(dir, "start.sh")
			require.NoError(t, os.WriteFile(script, data, 0600))
			secret := filepath.Join(dir, "secret.txt")
			target := filepath.Join(dir, "unrelated.txt")
			if kind == "existing readable" {
				require.NoError(t, os.WriteFile(secret, []byte(strings.Repeat("a", 64)), 0644))
				require.NoError(t, os.Chmod(secret, 0644))
			}
			if kind == "symlink" {
				require.NoError(t, os.WriteFile(target, []byte("unrelated fixture"), 0644))
				require.NoError(t, os.Chmod(target, 0644))
				require.NoError(t, os.Symlink(target, secret))
			}
			bin := filepath.Join(dir, "bin")
			require.NoError(t, os.Mkdir(bin, 0700))
			// Stub Docker and sleeps: the real deployment is never invoked.
			require.NoError(t, os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\ntouch \"$DEPLOY_MARKER\"\n"), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(bin, "sleep"), []byte("#!/bin/sh\nexit 0\n"), 0700))
			marker := filepath.Join(dir, "deployed")
			cmd := exec.Command("bash", "-c", `umask 022; exec bash "$1"`, "fixture", script)
			cmd.Env = []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "DEPLOY_MARKER=" + marker}
			log, err := cmd.CombinedOutput()
			if kind == "symlink" {
				require.Error(t, err, "symlink must be rejected before reading a credential")
				info, err := os.Stat(target)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0644), info.Mode().Perm())
				_, err = os.Stat(marker)
				require.True(t, os.IsNotExist(err), "unsafe secret must not start deployment")
				return
			}
			require.NoError(t, err, "%s", log)
			info, err := os.Stat(secret)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm(), "%s JWT secret must be private", kind)
			if kind == "existing readable" {
				data, err := os.ReadFile(secret)
				require.NoError(t, err)
				require.Equal(t, strings.Repeat("a", 64), string(data), "existing secret must not be replaced")
			}
		})
	}
}
