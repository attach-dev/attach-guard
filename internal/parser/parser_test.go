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
		// Shell operators without spaces
		{"ls&&npm install axios", []string{"ls", "&&", "npm", "install", "axios"}},
		{"ls||npm install axios", []string{"ls", "||", "npm", "install", "axios"}},
		{"ls;npm install axios", []string{"ls", ";", "npm", "install", "axios"}},
		{"ls&npm install axios", []string{"ls", "&", "npm", "install", "axios"}},
		{"ls|npm install axios", []string{"ls", "|", "npm", "install", "axios"}},
		// Newlines as command separators
		{"echo hello\nnpm install axios", []string{"echo", "hello", ";", "npm", "install", "axios"}},
		// Operators inside quotes should NOT be split
		{`echo "a&&b"`, []string{"echo", "a&&b"}},
		{`echo 'a;b'`, []string{"echo", "a;b"}},
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
		name      string
		command   string
		isInstall bool
		pkgCount  int
		pkgName   string
		pkgPinned bool
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
		name     string
		command  string
		pkgName  string
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
		name     string
		command  string
		pkgName  string
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

func TestParseAll_CapturesNestedShellSegments(t *testing.T) {
	results := ParseAll("bash -c 'npm install safe-pkg && pnpm add evil-pkg'")
	if len(results) != 2 {
		t.Fatalf("ParseAll returned %d commands, want 2", len(results))
	}
	if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "safe-pkg" {
		t.Fatalf("first parsed command = %#v, want npm install safe-pkg", results[0])
	}
	if results[1].PackageManager != "pnpm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "evil-pkg" {
		t.Fatalf("second parsed command = %#v, want pnpm add evil-pkg", results[1])
	}
}

func TestParseAll_CapturesWrappedLaterShellSegments(t *testing.T) {
	results := ParseAll("bash -c 'echo hi && env npm install lodash && sudo pnpm add react'")
	if len(results) != 2 {
		t.Fatalf("ParseAll returned %d commands, want 2", len(results))
	}
	if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "lodash" {
		t.Fatalf("first parsed command = %#v, want env npm install lodash", results[0])
	}
	if results[1].PackageManager != "pnpm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "react" {
		t.Fatalf("second parsed command = %#v, want sudo pnpm add react", results[1])
	}
}

func TestParseAll_CapturesBackgroundedSegments(t *testing.T) {
	results := ParseAll("echo hi & npm install lodash && bash -c 'echo done & pnpm add react'")
	if len(results) != 2 {
		t.Fatalf("ParseAll returned %d commands, want 2", len(results))
	}
	if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "lodash" {
		t.Fatalf("first parsed command = %#v, want npm install lodash", results[0])
	}
	if results[1].PackageManager != "pnpm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "react" {
		t.Fatalf("second parsed command = %#v, want pnpm add react", results[1])
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
		{"env split string npm install", "env -S 'npm install axios'", "axios"},
		{"env split string with assignment", "env -S 'NODE_ENV=production npm install axios'", "axios"},
		{"sudo pnpm add", "sudo pnpm add react", "react"},
		{"env pnpm add", "env pnpm add react", "react"},
		{"path-qualified sudo", "/usr/bin/sudo npm install axios", "axios"},
		{"stacked sudo", "sudo sudo npm install axios", "axios"},
		{"env then sudo", "env NODE_ENV=prod sudo npm install axios", "axios"},
		{"empty env var value", "VAR= npm install axios", "axios"},
		{"path-qualified env", "/usr/bin/env npm install axios", "axios"},
		{"command npm install", "command npm install axios", "axios"},
		{"time npm install", "time npm install axios", "axios"},
		{"nice npm install", "nice -n 10 npm install axios", "axios"},
		{"npx npm install", "npx npm install axios", "axios"},
		{"npx --yes npm install", "npx --yes npm install axios", "axios"},
		{"command -v skipped flags", "command npm install axios", "axios"},
		{"bash -c npm install", "bash -c 'npm install axios'", "axios"},
		{"sh -c npm install", "sh -c 'npm install axios'", "axios"},
		{"zsh -c npm install", "zsh -lc 'npm install axios'", "axios"},
		{"bash -c pnpm add", "bash -c 'pnpm add react'", "react"},
		{"sh -c with double quotes", `sh -c "npm install lodash"`, "lodash"},
		{"sudo bash -c", "sudo bash -c 'npm install axios'", "axios"},
		{"bash -c with chained cmds", "bash -c 'npm install axios && npm install lodash'", "axios"},
		{"sh -c with semicolon chain", "sh -c 'npm install axios; echo done'", "axios"},
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
		"env -S 'echo hello'",
		"npx create-react-app my-app",
		"command -v npm",
		"time echo hello",
		"bash script.sh npm install axios",
		`echo "npm install axios"`,
		"bash -c 'echo hello'",
		"bash -c 'echo hello' npm install axios",
	}
	for _, cmd := range nonInstalls {
		if result := Parse(cmd); result != nil {
			t.Errorf("Parse(%q) should return nil", cmd)
		}
	}
}

func TestLooksLikeInstall(t *testing.T) {
	suspicious := []string{
		"some-wrapper npm install axios",
		"/opt/bin/mystery npm install lodash",
		"strace npm install axios",
		"nohup npm install axios",
		"strace pip --proxy http://proxy.example install flask",
		"strace pip -i https://custom.example/simple install flask",
		"strace cargo --color always add serde",
		"watch pnpm add react",
		"strace bash -c 'npm install axios'",
		"ltrace sh -c 'pnpm add react'",
		"nohup bash -lc 'npm install lodash'",
		"env -S 'npm install axios'",
	}
	for _, cmd := range suspicious {
		if !LooksLikeInstall(cmd) {
			t.Errorf("LooksLikeInstall(%q) = false, want true", cmd)
		}
	}

	safe := []string{
		"git status",
		"npm run test",
		"npm test",
		"echo install npm",
		"ls -la",
		"pnpm run build",
		`echo "npm install axios"`,
		`env -S "echo hello"`,
		`bash -c "echo hello"`,
		"bash script.sh npm install axios",
		"echo npm install axios",
		"cat npm install axios",
		"printf npm install axios",
		"grep npm install package.json",
		"python -c 'npm install'",
		"node -e 'npm install'",
		"bash -c 'echo hello' npm install axios",
		"sh -c 'ls' npm install lodash",
	}
	for _, cmd := range safe {
		if LooksLikeInstall(cmd) {
			t.Errorf("LooksLikeInstall(%q) = true, want false", cmd)
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
		"pip install requests",
		"go get golang.org/x/net",
		"cargo add serde",
		"pip install .",
		"go get ./...",
		"cargo add --git https://github.com/user/repo",
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
		"pip --version",
		"go build ./...",
		"cargo build",
	}
	for _, cmd := range nonInstallCmds {
		if IsInstallCommand(cmd) {
			t.Errorf("IsInstallCommand(%q) = true, want false", cmd)
		}
	}
}

func TestParse_MultiEcosystemCommands(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		wantPM       string
		wantCount    int
		wantName     string
		wantVersion  string
		wantPinned   bool
		wantUnparsed bool
		wantNonLocal bool
	}{
		{"pip basic", "pip install requests", "pip", 1, "requests", "", false, false, false},
		{"pip pre action proxy deferred", "pip --proxy http://proxy.example install requests", "pip", 0, "", "", false, true, true},
		{"pip assignment source deferred", "pip install requests --index-url=https://custom.pypi.org/simple", "pip", 0, "", "", false, true, true},
		{"pip deferred path", "pip install .", "pip", 0, "", "", false, true, false},
		{"pip local find links keeps package evaluation", "pip install --find-links ./dist flask", "pip", 1, "flask", "", false, true, false},
		{"pip remote vcs deferred", "pip install git+https://github.com/user/repo.git", "pip", 0, "", "", false, true, true},
		{"pip custom index deferred", "pip install requests --index-url https://custom.pypi.org/simple", "pip", 0, "", "", false, true, true},
		{"pip inline local find links env", "PIP_FIND_LINKS=./dist pip install flask", "pip", 1, "flask", "", false, true, false},
		{"pip inline source env deferred", "PIP_INDEX_URL=https://private.example/simple pip install requests", "pip", 0, "", "", false, true, true},
		{"go exact", "go get golang.org/x/net@v0.25.0", "go", 1, "golang.org/x/net", "v0.25.0", true, false, false},
		{"go deferred local", "go get ./...", "go", 0, "", "", false, true, false},
		{"go deferred current module dot", "go get .", "go", 0, "", "", false, true, false},
		{"go inline private env deferred", "GOPRIVATE=private.example.com go get private.example.com/mod", "go", 0, "", "", false, true, true},
		{"cargo exact", "cargo add serde@=1.0.200", "cargo", 1, "serde", "1.0.200", true, false, false},
		{"cargo optional boolean flag", "cargo add --optional serde", "cargo", 1, "serde", "", false, false, false},
		{"cargo pre action color assignment", "cargo --color=always add serde", "cargo", 1, "serde", "", false, false, false},
		{"cargo deferred requirement", "cargo add serde@1.0.200", "cargo", 0, "", "", false, true, true},
		{"cargo custom registry deferred", "cargo add serde --registry internal", "cargo", 0, "", "", false, true, true},
		{"cargo custom registry assignment deferred", "cargo add serde --registry=internal", "cargo", 0, "", "", false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if result == nil {
				t.Fatalf("Parse(%q) returned nil", tt.command)
			}
			if result.PackageManager != tt.wantPM {
				t.Fatalf("Parse(%q).PackageManager = %q, want %q", tt.command, result.PackageManager, tt.wantPM)
			}
			if len(result.Packages) != tt.wantCount {
				t.Fatalf("Parse(%q) found %d packages, want %d", tt.command, len(result.Packages), tt.wantCount)
			}
			if result.HasUnparsedArgs != tt.wantUnparsed {
				t.Fatalf("Parse(%q).HasUnparsedArgs = %v, want %v", tt.command, result.HasUnparsedArgs, tt.wantUnparsed)
			}
			if result.HasNonLocalUnparsedArgs != tt.wantNonLocal {
				t.Fatalf("Parse(%q).HasNonLocalUnparsedArgs = %v, want %v", tt.command, result.HasNonLocalUnparsedArgs, tt.wantNonLocal)
			}
			if tt.wantCount > 0 {
				if result.Packages[0].Name != tt.wantName || result.Packages[0].Version != tt.wantVersion || result.Packages[0].Pinned != tt.wantPinned {
					t.Fatalf("Parse(%q).Packages[0] = %#v, want name=%q version=%q pinned=%v", tt.command, result.Packages[0], tt.wantName, tt.wantVersion, tt.wantPinned)
				}
			}
		})
	}
}
