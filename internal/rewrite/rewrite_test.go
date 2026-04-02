package rewrite

import (
	"testing"

	"github.com/attach-dev/attach-guard/pkg/api"
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
			name: "preserves post-action flags",
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
		{
			name: "preserves pre-action flags before action",
			cmd: &api.ParsedCommand{
				PackageManager: "pnpm",
				Action:         "add",
				PreActionFlags: []string{"--filter", "web"},
				Packages: []api.PackageRequest{
					{Name: "react", RawSpec: "react"},
				},
			},
			versions: map[string]string{"react": "18.2.0"},
			expected: "pnpm --filter web add react@18.2.0",
		},
			{
				name: "both pre and post action flags",
				cmd: &api.ParsedCommand{
				PackageManager: "npm",
				Action:         "install",
				PreActionFlags: []string{"--prefix", "./app"},
				Packages: []api.PackageRequest{
					{Name: "axios", RawSpec: "axios"},
				},
				Flags: []string{"--save-dev"},
			},
				versions: map[string]string{"axios": "1.7.0"},
				expected: "npm --prefix ./app install axios@1.7.0 --save-dev",
			},
			{
				name: "pip rewrite uses exact pin format",
				cmd: &api.ParsedCommand{
					PackageManager: "pip",
					Action:         "install",
					Packages: []api.PackageRequest{
						{Name: "requests", RawSpec: "requests"},
					},
				},
				versions: map[string]string{"requests": "2.31.0"},
				expected: "pip install requests==2.31.0",
			},
			{
				name: "go rewrite keeps v prefix",
				cmd: &api.ParsedCommand{
					PackageManager: "go",
					Action:         "get",
					Packages: []api.PackageRequest{
						{Name: "golang.org/x/net", RawSpec: "golang.org/x/net"},
					},
				},
				versions: map[string]string{"golang.org/x/net": "v0.25.0"},
				expected: "go get golang.org/x/net@v0.25.0",
			},
			{
				name: "cargo rewrite uses exact requirement syntax",
				cmd: &api.ParsedCommand{
					PackageManager: "cargo",
					Action:         "add",
					Packages: []api.PackageRequest{
						{Name: "serde", RawSpec: "serde"},
					},
				},
				versions: map[string]string{"serde": "1.0.200"},
				expected: "cargo add serde@=1.0.200",
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
