package rewrite

import (
	"testing"

	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
)

func TestCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmd      *api.ParsedCommand
		versions map[string]string
		expected string
	}{
		{
			name: "npm single rewrite",
			cmd: &api.ParsedCommand{
				PackageManager: "npm",
				Action:         "install",
				Packages: []api.PackageRequest{
					{Name: "axios", RawSpec: "axios"},
				},
			},
			versions: map[string]string{"axios": "1.7.0"},
			expected: "npm install axios@1.7.0",
		},
		{
			name: "npm multiple, one rewritten",
			cmd: &api.ParsedCommand{
				PackageManager: "npm",
				Action:         "install",
				Packages: []api.PackageRequest{
					{Name: "axios", RawSpec: "axios"},
					{Name: "lodash", RawSpec: "lodash@4.17.21"},
				},
			},
			versions: map[string]string{"axios": "1.7.0"},
			expected: "npm install axios@1.7.0 lodash@4.17.21",
		},
		{
			name: "pnpm add rewrite",
			cmd: &api.ParsedCommand{
				PackageManager: "pnpm",
				Action:         "add",
				Packages: []api.PackageRequest{
					{Name: "express", RawSpec: "express"},
				},
			},
			versions: map[string]string{"express": "4.18.2"},
			expected: "pnpm add express@4.18.2",
		},
		{
			name: "preserves flags",
			cmd: &api.ParsedCommand{
				PackageManager: "npm",
				Action:         "install",
				Packages: []api.PackageRequest{
					{Name: "jest", RawSpec: "jest"},
				},
				Flags: []string{"--save-dev"},
			},
			versions: map[string]string{"jest": "29.7.0"},
			expected: "npm install jest@29.7.0 --save-dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Command(tt.cmd, tt.versions)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}
