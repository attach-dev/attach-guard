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
		// Redirections without spaces
		{"npm install axios>install.log", []string{"npm", "install", "axios", ">", "install.log"}},
		{"npm install axios>2", []string{"npm", "install", "axios", ">", "2"}},
		{"npm install axios>=2", []string{"npm", "install", "axios", ">", "=2"}},
		{"npm install axios >> install.log", []string{"npm", "install", "axios", ">>", "install.log"}},
		{"npm install axios 2>err.log", []string{"npm", "install", "axios", "2>", "err.log"}},
		{"npm install axios 2>&1", []string{"npm", "install", "axios", "2>&", "1"}},
		{"npm install axios &>combined.log", []string{"npm", "install", "axios", "&>", "combined.log"}},
		{"npm install < packages.txt", []string{"npm", "install", "<", "packages.txt"}},
		{"cat <<EOF\nnpm install phantom\nEOF\npnpm add real", []string{"cat", "<<", "EOF", ";", "pnpm", "add", "real"}},
		// Command substitutions are kept as one token so inner spaces/operators
		// do not become outer argv or command separators.
		{"npm install $(echo axios)", []string{"npm", "install", "$(echo axios)"}},
		{"echo $(npm install axios)&&pnpm add zod", []string{"echo", "$(npm install axios)", "&&", "pnpm", "add", "zod"}},
		{"echo `npm install axios`&&pnpm add zod", []string{"echo", "`npm install axios`", "&&", "pnpm", "add", "zod"}},
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

func TestParse_RedirectionsNotPackageNames(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantPM  string
		wantPkg string
	}{
		{"npm stdout spaced", "npm install axios > install.log", "npm", "axios"},
		{"npm stdout attached", "npm install axios>install.log", "npm", "axios"},
		{"pnpm stderr fd", "pnpm add react 2>err.log", "pnpm", "react"},
		{"pip stdin", "pip install requests < packages.txt", "pip", "requests"},
		{"go append stdout", "go get golang.org/x/net >>install.log", "go", "golang.org/x/net"},
		{"cargo fd duplicate", "cargo add serde 2>&1", "cargo", "serde"},
		{"leading redirection", "2>err.log npm install lodash", "npm", "lodash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if result == nil {
				t.Fatalf("Parse(%q) returned nil", tt.command)
			}
			if result.PackageManager != tt.wantPM {
				t.Fatalf("PackageManager = %q, want %q", result.PackageManager, tt.wantPM)
			}
			if len(result.Packages) != 1 || result.Packages[0].Name != tt.wantPkg {
				t.Fatalf("Packages = %#v, want one package %q", result.Packages, tt.wantPkg)
			}
			if !result.HasUnparsedArgs {
				t.Fatalf("HasUnparsedArgs = false, want true so redirection is not rewritten away")
			}
			for _, pkg := range result.Packages {
				switch pkg.Name {
				case ">", ">>", "<", "2>", "2>&", "&>", "install.log", "err.log", "packages.txt", "1":
					t.Fatalf("redirection token/target %q should not be a package", pkg.Name)
				}
			}
		})
	}
}

func TestParseAll_RedirectionsAcrossChainedSegments(t *testing.T) {
	results := ParseAll("npm install axios > npm.log && pnpm add react 2>pnpm.err")
	if len(results) != 2 {
		t.Fatalf("ParseAll returned %d commands, want 2", len(results))
	}
	if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "axios" {
		t.Fatalf("first parsed command = %#v, want npm install axios", results[0])
	}
	if results[1].PackageManager != "pnpm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "react" {
		t.Fatalf("second parsed command = %#v, want pnpm add react", results[1])
	}
}

func TestParseAll_HereDocBodiesAreData(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantPM  string
		wantPkg string
	}{
		{
			name:    "plain heredoc body is not a command",
			command: "cat <<EOF\nnpm install phantom\nEOF",
		},
		{
			name:    "install command with heredoc keeps only command argv",
			command: "npm install safe-pkg <<EOF\nnpm install phantom\nEOF",
			wantPM:  "npm",
			wantPkg: "safe-pkg",
		},
		{
			name:    "command after heredoc delimiter is still parsed",
			command: "cat <<EOF\nnpm install phantom\nEOF\npnpm add real-pkg",
			wantPM:  "pnpm",
			wantPkg: "real-pkg",
		},
		{
			name:    "tab-stripping heredoc body is not a command",
			command: "cat <<-EOF\n\tnpm install phantom\n\tEOF",
		},
		{
			name:    "quoted heredoc delimiter suppresses command substitutions",
			command: "cat <<'EOF'\n$(npm install literal-pkg)\nEOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := ParseAll(tt.command)
			if tt.wantPM == "" {
				if len(results) != 0 {
					t.Fatalf("ParseAll(%q) returned %#v, want no install commands", tt.command, results)
				}
				if LooksLikeInstall(tt.command) {
					t.Fatalf("LooksLikeInstall(%q) = true, want false", tt.command)
				}
				return
			}
			if len(results) != 1 {
				t.Fatalf("ParseAll(%q) returned %d commands, want 1", tt.command, len(results))
			}
			if results[0].PackageManager != tt.wantPM || len(results[0].Packages) != 1 || results[0].Packages[0].Name != tt.wantPkg {
				t.Fatalf("parsed command = %#v, want %s install %s", results[0], tt.wantPM, tt.wantPkg)
			}
		})
	}
}

func TestParse_CommandSubstitutionDynamicArgsNotPackages(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantPM  string
	}{
		{"npm", "npm install $(echo axios)", "npm"},
		{"pnpm", "pnpm add $(printf react)", "pnpm"},
		{"pip", "pip install $(echo requests)", "pip"},
		{"go", "go get $(echo golang.org/x/net)", "go"},
		{"cargo", "cargo add $(echo serde)", "cargo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.command)
			if result == nil {
				t.Fatalf("Parse(%q) returned nil", tt.command)
			}
			if result.PackageManager != tt.wantPM {
				t.Fatalf("PackageManager = %q, want %q", result.PackageManager, tt.wantPM)
			}
			if len(result.Packages) != 0 {
				t.Fatalf("Packages = %#v, want none for dynamic command-substitution args", result.Packages)
			}
			if !result.HasUnparsedArgs || !result.HasNonLocalUnparsedArgs {
				t.Fatalf("unparsed flags = (%v, %v), want both true", result.HasUnparsedArgs, result.HasNonLocalUnparsedArgs)
			}
		})
	}
}

func TestParseAll_CommandSubstitutionInDiscardedPrefixes(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "inline env assignment",
			command: "FOO=$(npm install evil-pkg) npm install safe-pkg",
		},
		{
			name:    "sudo user argument",
			command: "sudo -u $(npm install evil-pkg) npm install safe-pkg",
		},
		{
			name:    "shell positional argument",
			command: `bash -c 'npm install safe-pkg' "$(npm install evil-pkg)"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := ParseAll(tt.command)
			if len(results) != 2 {
				t.Fatalf("ParseAll(%q) returned %d commands, want 2: %#v", tt.command, len(results), results)
			}
			if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "evil-pkg" {
				t.Fatalf("first parsed command = %#v, want discarded-prefix npm install evil-pkg", results[0])
			}
			if results[1].PackageManager != "npm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "safe-pkg" {
				t.Fatalf("second parsed command = %#v, want retained npm install safe-pkg", results[1])
			}
		})
	}
}

func TestParseAll_CommandSubstitutionCapturesNestedInstall(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"dollar parens", "echo $(npm install evil-pkg) && pnpm add safe-pkg"},
		{"backticks", "echo `npm install evil-pkg` && pnpm add safe-pkg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := ParseAll(tt.command)
			if len(results) != 2 {
				t.Fatalf("ParseAll returned %d commands, want 2", len(results))
			}
			if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "evil-pkg" {
				t.Fatalf("first parsed command = %#v, want nested npm install evil-pkg", results[0])
			}
			if results[1].PackageManager != "pnpm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "safe-pkg" {
				t.Fatalf("second parsed command = %#v, want pnpm add safe-pkg", results[1])
			}
		})
	}
}

func TestParseAll_CommandSubstitutionInsideUnquotedHereDoc(t *testing.T) {
	results := ParseAll("cat <<EOF\n$(npm install evil-pkg)\nEOF")
	if len(results) != 1 {
		t.Fatalf("ParseAll returned %d commands, want 1: %#v", len(results), results)
	}
	if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "evil-pkg" {
		t.Fatalf("parsed command = %#v, want heredoc substitution npm install evil-pkg", results[0])
	}
	if !LooksLikeInstall("cat <<EOF\n$(npm install evil-pkg)\nEOF") {
		t.Fatal("LooksLikeInstall returned false for install hidden inside unquoted heredoc substitution")
	}
	if LooksLikeInstall("cat <<EOF\n\\$(npm install escaped-pkg)\nEOF") {
		t.Fatal("LooksLikeInstall returned true for escaped heredoc substitution")
	}
	if !LooksLikeInstall("cat <<EOF\n\\\\$(npm install even-backslash-pkg)\nEOF") {
		t.Fatal("LooksLikeInstall returned false for heredoc substitution after even backslashes")
	}
	if !LooksLikeInstall("cat <<EOF\n`npm install backtick-pkg`\nEOF") {
		t.Fatal("LooksLikeInstall returned false for backtick install hidden inside unquoted heredoc")
	}
	if !LooksLikeInstall("cat <<EOF\n$(\nnpm install multiline-pkg\n)\nEOF") {
		t.Fatal("LooksLikeInstall returned false for multiline command substitution inside unquoted heredoc")
	}
}

func TestParseAll_CommandSubstitutionInsideUnquotedHereDocWithFollowingCommand(t *testing.T) {
	results := ParseAll("cat <<EOF\n$(npm install safe-pkg)\nEOF\npnpm add evil-pkg")
	if len(results) != 2 {
		t.Fatalf("ParseAll returned %d commands, want heredoc substitution and following command: %#v", len(results), results)
	}
	if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "safe-pkg" {
		t.Fatalf("first parsed command = %#v, want heredoc npm install safe-pkg", results[0])
	}
	if results[1].PackageManager != "pnpm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "evil-pkg" {
		t.Fatalf("second parsed command = %#v, want following pnpm add evil-pkg", results[1])
	}
}

func TestParseAll_CommandSubstitutionInPreActionFlagPreservesOuterInstall(t *testing.T) {
	results := ParseAll("npm --prefix=$(npm install safe-pkg >/dev/null; printf .) install bad-pkg")
	if len(results) != 2 {
		t.Fatalf("ParseAll returned %d commands, want inner and outer npm installs: %#v", len(results), results)
	}
	if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "safe-pkg" {
		t.Fatalf("first parsed command = %#v, want inner npm install safe-pkg", results[0])
	}
	if results[1].PackageManager != "npm" || !results[1].HasNonLocalUnparsedArgs {
		t.Fatalf("second parsed command = %#v, want outer npm install marked non-local/dynamic", results[1])
	}
}

func TestParseAll_SuspiciousSubstitutionDoesNotHideCleanSegment(t *testing.T) {
	results := ParseAll("FOO=$(strace npm install evil-pkg) npm install safe-pkg")
	if len(results) != 2 {
		t.Fatalf("ParseAll returned %d commands, want suspicious substitution plus outer install: %#v", len(results), results)
	}
	if results[0].PackageManager != "npm" || !results[0].HasNonLocalUnparsedArgs || len(results[0].Packages) != 0 {
		t.Fatalf("first parsed command = %#v, want suspicious unparsed npm install", results[0])
	}
	if results[1].PackageManager != "npm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "safe-pkg" {
		t.Fatalf("second parsed command = %#v, want outer npm install safe-pkg", results[1])
	}
}

func TestParseAll_ProcessSubstitutionCapturesNestedInstall(t *testing.T) {
	tests := []string{
		"cat <(npm install evil-pkg)",
		"cat >(npm install evil-pkg)",
	}
	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			results := ParseAll(cmd)
			if len(results) != 1 {
				t.Fatalf("ParseAll returned %d commands, want process-substitution install: %#v", len(results), results)
			}
			if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "evil-pkg" {
				t.Fatalf("parsed command = %#v, want process-substitution npm install evil-pkg", results[0])
			}
			if !LooksLikeInstall(cmd) {
				t.Fatalf("LooksLikeInstall(%q) = false, want true", cmd)
			}
		})
	}
}

func TestParseAll_SuppressedWrapperSubstitutionDoesNotHideCleanSegment(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"command introspection", "command -v $(npm install evil-pkg) && npm install safe-pkg"},
		{"uv non-pip", "uv add $(npm install evil-pkg) && npm install safe-pkg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := ParseAll(tt.command)
			if len(results) != 2 {
				t.Fatalf("ParseAll returned %d commands, want hidden substitution plus outer install: %#v", len(results), results)
			}
			if results[0].PackageManager != "npm" || len(results[0].Packages) != 1 || results[0].Packages[0].Name != "evil-pkg" {
				t.Fatalf("first parsed command = %#v, want hidden npm install evil-pkg", results[0])
			}
			if results[1].PackageManager != "npm" || len(results[1].Packages) != 1 || results[1].Packages[0].Name != "safe-pkg" {
				t.Fatalf("second parsed command = %#v, want outer npm install safe-pkg", results[1])
			}
		})
	}
}

func TestParseAll_CommandSubstitutionAsCommandPositionFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"dynamic package-manager", "$(printf npm) install leftpad"},
		{"dynamic package-manager with separate-value flag", "$(printf npm) --prefix app install leftpad"},
		{"dynamic package-manager and action", "$(printf 'npm install') leftpad"},
		{"dynamic full argv", "$(printf 'npm install leftpad')"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := ParseAll(tt.command)
			if len(results) != 1 {
				t.Fatalf("ParseAll returned %d commands, want suspicious synthesized install: %#v", len(results), results)
			}
			if results[0].PackageManager != "dynamic-shell" || !results[0].HasNonLocalUnparsedArgs || len(results[0].Packages) != 0 {
				t.Fatalf("parsed command = %#v, want suspicious unparsed dynamic-shell install", results[0])
			}
			if !LooksLikeInstall(tt.command) {
				t.Fatalf("LooksLikeInstall(%q) = false, want true", tt.command)
			}
		})
	}
}

func TestLooksLikeInstall_CommandSubstitutionWrapperBypass(t *testing.T) {
	if !LooksLikeInstall("echo $(some-wrapper npm install evil-pkg)") {
		t.Fatal("LooksLikeInstall returned false for install hidden inside command substitution")
	}
	if !LooksLikeInstall("echo $(echo $(echo $(echo $(echo $(npm install depth-limit-pkg)))))") {
		t.Fatal("LooksLikeInstall returned false for command substitution at depth limit")
	}
	if LooksLikeInstall("echo '$(npm install literal-pkg)'") {
		t.Fatal("LooksLikeInstall returned true for single-quoted literal command substitution")
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
		{"uv pip install", "uv pip install requests", "requests"},
		{"uv --quiet pip install", "uv --quiet pip install requests", "requests"},
		{"uv -p pip install", "uv -p 3.13 pip install requests", "requests"},
		{"uv --project pip install", "uv --project /tmp pip install requests", "requests"},
		{"uv --directory assignment pip install", "uv --directory=/tmp pip install requests", "requests"},
		{"uv pip install python flag", "uv pip install -p 3.13 requests", "requests"},
		{"uv pip install python assignment flag", "uv pip install --python 3.13 requests", "requests"},
		{"sudo uv pip install", "sudo uv pip install requests", "requests"},
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
		"uv add requests",
		"uv sync",
		"uv --project /tmp add requests",
		"uv --directory=/tmp sync",
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
		"strace uv pip install requests",
		"strace uv -p 3.13 pip install requests",
		"strace uv --project /tmp pip install requests",
		"strace uv --directory=/tmp pip install requests",
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
		// attach-guard's own evaluate command should not trigger
		"attach-guard evaluate npm install axios",
		"/path/to/attach-guard-darwin-amd64 evaluate npm install axios",
		`"/Users/me/.claude/plugins/attach-dev/plugin/hooks/bin/attach-guard-darwin-arm64" evaluate npm install axios`,
		"uv sync",
		"uv add requests",
		"uv",
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
		"go install golang.org/x/tools/cmd/godoc@latest",
		"cargo install ripgrep",
		"uv pip install requests",
		"uv --project /tmp pip install requests",
		"cargo install ripgrep --version 14.0.0",
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
		"uv add requests",
		"uv sync",
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
		{"pip local find links deferred", "pip install --find-links ./dist flask", "pip", 0, "", "", false, true, true},
		{"pip remote vcs deferred", "pip install git+https://github.com/user/repo.git", "pip", 0, "", "", false, true, true},
		{"pip greater-than shell redirection", "pip install requests>2.0", "pip", 1, "requests", "", false, true, false},
		{"pip custom index deferred", "pip install requests --index-url https://custom.pypi.org/simple", "pip", 0, "", "", false, true, true},
		{"pip inline file index env deferred", "PIP_INDEX_URL=file:///tmp/simple pip install requests", "pip", 0, "", "", false, true, true},
		{"pip inline local find links env", "PIP_FIND_LINKS=./dist pip install flask", "pip", 0, "", "", false, true, true},
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
		{"go install pinned", "go install golang.org/x/tools/cmd/godoc@v0.20.0", "go", 1, "golang.org/x/tools/cmd/godoc", "v0.20.0", true, false, false},
		{"go install unpinned", "go install golang.org/x/tools/cmd/godoc", "go", 1, "golang.org/x/tools/cmd/godoc", "", false, false, false},
		{"go install local", "go install ./...", "go", 0, "", "", false, true, false},
		{"go install current module dot", "go install .", "go", 0, "", "", false, true, false},
		{"cargo install basic", "cargo install ripgrep", "cargo", 1, "ripgrep", "", false, false, false},
		{"cargo install unknown pre action flag", "cargo --mystery value install ripgrep", "cargo", 0, "", "", false, true, true},
		{"cargo install pre action color", "cargo --color always install ripgrep", "cargo", 1, "ripgrep", "", false, false, false},
		{"cargo install pre action color assignment", "cargo --color=always install ripgrep", "cargo", 1, "ripgrep", "", false, false, false},
		{"cargo install version before package", "cargo install --version 14.0.0 ripgrep", "cargo", 1, "ripgrep", "14.0.0", true, false, false},
		{"cargo install path deferred", "cargo install --path ./local", "cargo", 0, "", "", false, true, false},
		{"cargo install git deferred", "cargo install --git https://github.com/user/repo", "cargo", 0, "", "", false, true, true},
		{"cargo install version flag", "cargo install ripgrep --version 14.0.0", "cargo", 1, "ripgrep", "14.0.0", true, false, false},
		{"cargo install multi pkg version ambiguous", "cargo install ripgrep fd-find --version 1.2.3", "cargo", 2, "ripgrep", "", false, true, true},
		{"uv pip install basic", "uv pip install requests", "pip", 1, "requests", "", false, false, false},
		{"uv pip install pinned", "uv pip install requests==2.31.0", "pip", 1, "requests", "2.31.0", true, false, false},
		{"uv pip install python flag", "uv pip install -p 3.13 requests", "pip", 1, "requests", "", false, false, false},
		{"uv pip install python long flag", "uv pip install --python 3.13 requests", "pip", 1, "requests", "", false, false, false},
		{"uv wrapper python flag", "uv -p 3.13 pip install requests", "pip", 1, "requests", "", false, false, false},
		{"uv pip install project flag", "uv --project /tmp pip install requests", "pip", 1, "requests", "", false, false, false},
		{"uv pip install directory assignment", "uv --directory=/tmp pip install requests", "pip", 1, "requests", "", false, false, false},
		{"sudo uv pip install", "sudo uv pip install requests", "pip", 1, "requests", "", false, false, false},
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
