package openscore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

type LocalProvider struct {
	command string
	args    []string
}

func NewLocal(command string) (*LocalProvider, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		command = "attach-open-score"
	}
	resolvedCommand, args := resolveCommand(command)
	return &LocalProvider{command: resolvedCommand, args: args}, nil
}

func (p *LocalProvider) Name() string {
	return "open-score"
}

func (p *LocalProvider) IsAvailable(ctx context.Context) bool {
	if strings.ContainsRune(p.command, os.PathSeparator) {
		info, err := os.Stat(p.command)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(p.command)
	return err == nil && ctx.Err() == nil
}

func (p *LocalProvider) GetPackageScore(ctx context.Context, ecosystem api.Ecosystem, name, version string) (*api.VersionInfo, error) {
	args := append([]string{}, p.args...)
	args = append(args,
		"package",
		"--ecosystem", string(openScoreRequestEcosystem(ecosystem)),
		"--name", name,
		"--version", version,
	)
	cmd := exec.CommandContext(ctx, p.command, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return unavailableVersion(version), nil
	}

	var response verdictResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return unavailableVersion(version), nil
	}
	return versionInfoFromResponse(version, response), nil
}

func (p *LocalProvider) ListVersions(_ context.Context, _ api.Ecosystem, _ string) ([]api.VersionInfo, error) {
	return nil, provider.ErrUnsupportedSource
}

func (p *LocalProvider) String() string {
	return fmt.Sprintf("open-score command %q", p.command)
}

func resolveCommand(command string) (string, []string) {
	if command != "attach-open-score" {
		return command, nil
	}
	if _, err := exec.LookPath(command); err == nil {
		return command, nil
	}
	if root := findSiblingOpenScore(); root != "" {
		return "go", []string{"run", filepath.Join(root, "cmd", "attach-open-score")}
	}
	return command, nil
}

func findSiblingOpenScore() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for _, candidate := range []string{
		filepath.Join(cwd, "attach-open-score"),
		filepath.Join(cwd, "..", "attach-open-score"),
		filepath.Join(cwd, "..", "..", "attach-open-score"),
	} {
		if info, err := os.Stat(filepath.Join(candidate, "go.mod")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
