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

func TestParse_NPM_PreActionFlags(t *testing.T) {
	tests := []struct {
		name    string
		command string
		pkgName string
		preFlags int
	}{
		{"npm prefix flag", "npm --prefix ./app install axios", "axios", 2},
		{"npm legacy peer deps", "npm --legacy-peer-deps install lodash", "lodash", 1},
		{"npm verbose", "npm --verbose install lodash", "lodash", 1},
		{"npm registry flag", "npm --registry https://r.example.com install axios", "axios", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if result == nil {
				t.Fatalf("Parse(%q) returned nil, expected install command", tt.command)
			}
			if len(result.Packages) != 1 || result.Packages[0].Name != tt.pkgName {
				t.Errorf("Parse(%q).Packages[0].Name = %v, want %q", tt.command, result.Packages, tt.pkgName)
			}
			if len(result.PreActionFlags) != tt.preFlags {
				t.Errorf("Parse(%q).PreActionFlags = %v (len %d), want len %d", tt.command, result.PreActionFlags, len(result.PreActionFlags), tt.preFlags)
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

func TestParse_PNPM_PreActionFlags(t *testing.T) {
	tests := []struct {
		name    string
		command string
		pkgName string
	}{
		{"pnpm filter add", "pnpm --filter web add react", "react"},
		{"pnpm dir add", "pnpm --dir apps/web add zod", "zod"},
		{"pnpm -C add", "pnpm -C apps/web add zod", "zod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if result == nil {
				t.Fatalf("Parse(%q) returned nil, expected install command", tt.command)
			}
			if len(result.Packages) != 1 || result.Packages[0].Name != tt.pkgName {
				t.Errorf("Parse(%q).Packages[0].Name = %v, want %q", tt.command, result.Packages, tt.pkgName)
			}
			if len(result.PreActionFlags) == 0 {
				t.Errorf("Parse(%q).PreActionFlags should not be empty", tt.command)
			}
		})
	}
}

func TestParse_ShellOperators(t *testing.T) {
	tests := []struct {
		name    string
		command string
		pkgName string
		pkgCount int
	}{
		{"chained &&", "npm install axios && npm install lodash", "axios", 1},
		{"chained semicolon", "npm install axios; npm install lodash", "axios", 1},
		{"piped", "npm install axios | tee log.txt", "axios", 1},
		{"or chain", "npm install axios || echo failed", "axios", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if result == nil {
				t.Fatalf("Parse(%q) returned nil", tt.command)
			}
			if len(result.Packages) != tt.pkgCount {
				t.Errorf("Parse(%q) found %d packages, want %d", tt.command, len(result.Packages), tt.pkgCount)
			}
			if result.Packages[0].Name != tt.pkgName {
				t.Errorf("Parse(%q).Packages[0].Name = %q, want %q", tt.command, result.Packages[0].Name, tt.pkgName)
			}
		})
	}
}

func TestParse_ShellOperators_NotPackageNames(t *testing.T) {
	// Ensure shell operators are NOT treated as package names
	result := Parse("npm install axios && npm install lodash")
	if result == nil {
		t.Fatal("expected parsed result")
	}
	for _, pkg := range result.Packages {
		if pkg.Name == "&&" || pkg.Name == "npm" || pkg.Name == "install" || pkg.Name == "lodash" {
			t.Errorf("shell operator or second command token %q should not be a package name", pkg.Name)
		}
	}
}

func TestParse_CommandPrefixes(t *testing.T) {
	tests := []struct {
		name    string
		command string
		pkgName string
	}{
		{"sudo npm install", "sudo npm install axios", "axios"},
		{"sudo -E npm install", "sudo -E npm install axios", "axios"},
		{"sudo -u root npm install", "sudo -u root npm install axios", "axios"},
		{"env npm install", "env npm install axios", "axios"},
		{"env VAR=val npm install", "env NODE_ENV=production npm install axios", "axios"},
		{"inline env var", "NODE_ENV=production npm install axios", "axios"},
		{"multiple env vars", "NODE_ENV=production CI=true npm install axios", "axios"},
		{"sudo pnpm add", "sudo pnpm add react", "react"},
		{"env pnpm add", "env pnpm add react", "react"},
		{"path-qualified sudo", "/usr/bin/sudo npm install axios", "axios"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if result == nil {
				t.Fatalf("Parse(%q) returned nil, expected install command", tt.command)
			}
			if len(result.Packages) != 1 || result.Packages[0].Name != tt.pkgName {
				t.Errorf("Parse(%q).Packages[0].Name = %v, want %q", tt.command, result.Packages, tt.pkgName)
			}
		})
	}
}

func TestParse_CommandPrefixes_NonInstall(t *testing.T) {
	// These should not be treated as install commands
	nonInstalls := []string{
		"sudo ls -la",
		"env echo hello",
		"FOO=bar echo test",
	}
	for _, cmd := range nonInstalls {
		if result := Parse(cmd); result != nil {
			t.Errorf("Parse(%q) should return nil", cmd)
		}
	}
}

func TestIsInstallCommand(t *testing.T) {
	installCmds := []string{
		"npm install axios",
		"npm i lodash",
		"pnpm add express",
		"pnpm --filter web add react",
		"npm --prefix ./app install axios",
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
