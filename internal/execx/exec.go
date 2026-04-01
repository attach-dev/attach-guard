// Package execx provides safe subprocess execution helpers.
package execx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FindRealBinary locates the real binary for a package manager,
// skipping any attach-guard shim directory.
func FindRealBinary(name string) (string, error) {
	shimDir := ShimDir()

	// Search PATH, skipping our shim directory
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == shimDir {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not find %s in PATH (excluding shim directory)", name)
}

// ShimDir returns the directory where attach-guard shims are installed.
func ShimDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".attach-guard", "bin")
}

// RunPassthrough executes a command, passing through stdin/stdout/stderr.
func RunPassthrough(binary string, args []string) error {
	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IsRecursionGuarded checks if the recursion guard env var is set.
func IsRecursionGuarded() bool {
	return os.Getenv("ATTACH_GUARD_ACTIVE") == "1"
}

// SetRecursionGuard sets the recursion guard env var.
func SetRecursionGuard() {
	os.Setenv("ATTACH_GUARD_ACTIVE", "1")
}

// BuildCleanPATH returns PATH with the shim directory removed.
func BuildCleanPATH() string {
	shimDir := ShimDir()
	pathEnv := os.Getenv("PATH")
	var parts []string
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir != shimDir {
			parts = append(parts, dir)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}
