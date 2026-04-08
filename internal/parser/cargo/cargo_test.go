package cargo

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name         string
		command      []string
		wantNil      bool
		wantCount    int
		wantName     string
		wantVersion  string
		wantPinned   bool
		wantUnparsed bool
		wantNonLocal bool
	}{
		{"basic", []string{"cargo", "add", "serde"}, false, 1, "serde", "", false, false, false},
		{"unknown pre action flag safety", []string{"cargo", "--mystery", "value", "add", "serde"}, false, 0, "", "", false, true, true},
		{"pre action color", []string{"cargo", "--color", "always", "add", "serde"}, false, 1, "serde", "", false, false, false},
		{"pre action color assignment", []string{"cargo", "--color=always", "add", "serde"}, false, 1, "serde", "", false, false, false},
		{"exact pin", []string{"cargo", "add", "serde@=1.0.200"}, false, 1, "serde", "1.0.200", true, false, false},
		{"requirement deferred", []string{"cargo", "add", "serde@1.0.200"}, false, 0, "", "", false, true, true},
		{"git deferred", []string{"cargo", "add", "--git", "https://github.com/user/repo", "serde"}, false, 0, "", "", false, true, true},
		{"git assignment deferred", []string{"cargo", "add", "serde", "--git=https://github.com/user/repo"}, false, 0, "", "", false, true, true},
		{"path deferred", []string{"cargo", "add", "--path", "./local-crate"}, false, 0, "", "", false, true, false},
		{"registry deferred", []string{"cargo", "add", "serde", "--registry", "internal"}, false, 0, "", "", false, true, true},
		{"registry assignment deferred", []string{"cargo", "add", "serde", "--registry=internal"}, false, 0, "", "", false, true, true},
		{"optional boolean flag", []string{"cargo", "add", "--optional", "serde"}, false, 1, "serde", "", false, false, false},
		{"short features flag", []string{"cargo", "add", "clap", "-F", "derive"}, false, 1, "clap", "", false, false, false},
		{"unknown flag safety", []string{"cargo", "add", "serde", "--mystery", "internal"}, false, 0, "", "", false, true, true},
		{"bare add", []string{"cargo", "add"}, false, 0, "", "", false, false, false},
		{"not add", []string{"cargo", "build"}, true, 0, "", "", false, false, false},
		{"install basic", []string{"cargo", "install", "ripgrep"}, false, 1, "ripgrep", "", false, false, false},
		{"install unknown pre action flag safety", []string{"cargo", "--mystery", "value", "install", "ripgrep"}, false, 0, "", "", false, true, true},
		{"install pre action color", []string{"cargo", "--color", "always", "install", "ripgrep"}, false, 1, "ripgrep", "", false, false, false},
		{"install pre action color assignment", []string{"cargo", "--color=always", "install", "ripgrep"}, false, 1, "ripgrep", "", false, false, false},
		{"install pinned spec", []string{"cargo", "install", "ripgrep@=14.0.0"}, false, 1, "ripgrep", "14.0.0", true, false, false},
		{"install version flag before package", []string{"cargo", "install", "--version", "14.0.0", "ripgrep"}, false, 1, "ripgrep", "14.0.0", true, false, false},
		{"install version flag", []string{"cargo", "install", "ripgrep", "--version", "14.0.0"}, false, 1, "ripgrep", "14.0.0", true, false, false},
		{"install version assignment", []string{"cargo", "install", "ripgrep", "--version=14.0.0"}, false, 1, "ripgrep", "14.0.0", true, false, false},
		{"install version multi pkg ambiguous", []string{"cargo", "install", "ripgrep", "fd-find", "--version", "1.2.3"}, false, 2, "ripgrep", "", false, true, true},
		{"install git deferred", []string{"cargo", "install", "--git", "https://github.com/user/repo"}, false, 0, "", "", false, true, true},
		{"install path deferred", []string{"cargo", "install", "--path", "./local"}, false, 0, "", "", false, true, false},
		{"bare install", []string{"cargo", "install"}, false, 0, "", "", false, false, false},
		{"add with version flag ignored", []string{"cargo", "add", "serde", "--version", "1.0.0"}, false, 1, "serde", "1.0.0", true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.command, "")
			if tt.wantNil {
				if got != nil {
					t.Fatalf("Parse() = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("Parse() = nil, want command")
			}
			if len(got.Packages) != tt.wantCount {
				t.Fatalf("len(Packages) = %d, want %d", len(got.Packages), tt.wantCount)
			}
			if got.HasUnparsedArgs != tt.wantUnparsed {
				t.Fatalf("HasUnparsedArgs = %v, want %v", got.HasUnparsedArgs, tt.wantUnparsed)
			}
			if got.HasNonLocalUnparsedArgs != tt.wantNonLocal {
				t.Fatalf("HasNonLocalUnparsedArgs = %v, want %v", got.HasNonLocalUnparsedArgs, tt.wantNonLocal)
			}
			if tt.wantCount > 0 {
				if got.Packages[0].Name != tt.wantName || got.Packages[0].Version != tt.wantVersion || got.Packages[0].Pinned != tt.wantPinned {
					t.Fatalf("first package = %#v, want name=%q version=%q pinned=%v", got.Packages[0], tt.wantName, tt.wantVersion, tt.wantPinned)
				}
			}
		})
	}
}
