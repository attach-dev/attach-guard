package parser

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"npm install axios", []string{"npm", "install", "axios"}},
		{"npm i lodash express", []string{"npm", "i", "lodash", "express"}},
		{`npm install "some package"`, []string{"npm", "install", "some package"}},
		{"pnpm add axios@1.7.0", []string{"pnpm", "add", "axios@1.7.0"}},
		{"npm install --save-dev jest", []string{"npm", "install", "--save-dev", "jest"}},
		{"", nil},
		{"npm install @scope/pkg@^2.0.0", []string{"npm", "install", "@scope/pkg@^2.0.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Tokenize(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Tokenize(%q) = %v, want %v", tt.input, result, tt.expected)
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("Tokenize(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParse_NPM(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		isInstall  bool
		pkgCount   int
		pkgName    string
		pkgPinned  bool
	}{
		{"npm install single", "npm install axios", true, 1, "axios", false},
		{"npm i alias", "npm i lodash", true, 1, "lodash", false},
		{"npm install pinned", "npm install axios@1.7.0", true, 1, "axios", true},
		{"npm install multiple", "npm install axios lodash", true, 2, "axios", false},
		{"npm install scoped", "npm install @types/node", true, 1, "@types/node", false},
		{"npm install scoped pinned", "npm install @types/node@20.0.0", true, 1, "@types/node", true},
		{"npm install with flags", "npm install --save-dev jest", true, 1, "jest", false},
		{"npm install caret", "npm install axios@^1.7.0", true, 1, "axios", false},
		{"npm run not install", "npm run test", false, 0, "", false},
		{"npm test", "npm test", false, 0, "", false},
		{"npm install no packages", "npm install", false, 0, "", false},
		{"not npm", "git status", false, 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if !tt.isInstall {
				if result != nil {
					t.Errorf("Parse(%q) should return nil for non-install", tt.command)
				}
				return
			}
			if result == nil {
				t.Fatalf("Parse(%q) returned nil, expected install command", tt.command)
			}
			if !result.IsInstall {
				t.Errorf("Parse(%q).IsInstall = false, want true", tt.command)
			}
			if len(result.Packages) != tt.pkgCount {
				t.Errorf("Parse(%q) found %d packages, want %d", tt.command, len(result.Packages), tt.pkgCount)
			}
			if tt.pkgCount > 0 {
				if result.Packages[0].Name != tt.pkgName {
					t.Errorf("Parse(%q).Packages[0].Name = %q, want %q", tt.command, result.Packages[0].Name, tt.pkgName)
				}
				if result.Packages[0].Pinned != tt.pkgPinned {
					t.Errorf("Parse(%q).Packages[0].Pinned = %v, want %v", tt.command, result.Packages[0].Pinned, tt.pkgPinned)
				}
			}
		})
	}
}

func TestParse_PNPM(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		isInstall bool
		pkgCount  int
		pkgName   string
	}{
		{"pnpm add", "pnpm add axios", true, 1, "axios"},
		{"pnpm add multiple", "pnpm add axios lodash", true, 2, "axios"},
		{"pnpm add pinned", "pnpm add axios@1.7.0", true, 1, "axios"},
		{"pnpm install no pkgs", "pnpm install", false, 0, ""},
		{"pnpm run", "pnpm run test", false, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if !tt.isInstall {
				if result != nil {
					t.Errorf("Parse(%q) should return nil for non-install", tt.command)
				}
				return
			}
			if result == nil {
				t.Fatalf("Parse(%q) returned nil", tt.command)
			}
			if len(result.Packages) != tt.pkgCount {
				t.Errorf("Parse(%q) found %d packages, want %d", tt.command, len(result.Packages), tt.pkgCount)
			}
			if tt.pkgCount > 0 && result.Packages[0].Name != tt.pkgName {
				t.Errorf("Parse(%q).Packages[0].Name = %q, want %q", tt.command, result.Packages[0].Name, tt.pkgName)
			}
		})
	}
}

func TestIsInstallCommand(t *testing.T) {
	installCmds := []string{
		"npm install axios",
		"npm i lodash",
		"pnpm add express",
	}
	for _, cmd := range installCmds {
		if !IsInstallCommand(cmd) {
			t.Errorf("IsInstallCommand(%q) = false, want true", cmd)
		}
	}

	nonInstallCmds := []string{
		"npm run test",
		"npm test",
		"git status",
		"ls -la",
		"pnpm run build",
		"npm install",
		"echo hello",
	}
	for _, cmd := range nonInstallCmds {
		if IsInstallCommand(cmd) {
			t.Errorf("IsInstallCommand(%q) = true, want false", cmd)
		}
	}
}
