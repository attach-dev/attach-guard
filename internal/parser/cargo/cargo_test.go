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
	}{
		{"basic", []string{"cargo", "add", "serde"}, false, 1, "serde", "", false, false},
		{"exact pin", []string{"cargo", "add", "serde@=1.0.200"}, false, 1, "serde", "1.0.200", true, false},
		{"requirement deferred", []string{"cargo", "add", "serde@1.0.200"}, false, 0, "", "", false, true},
		{"git deferred", []string{"cargo", "add", "--git", "https://github.com/user/repo", "serde"}, false, 0, "", "", false, true},
		{"path deferred", []string{"cargo", "add", "--path", "./local-crate"}, false, 0, "", "", false, true},
		{"known flag value not crate", []string{"cargo", "add", "serde", "--registry", "internal"}, false, 1, "serde", "", false, false},
		{"short features flag", []string{"cargo", "add", "clap", "-F", "derive"}, false, 1, "clap", "", false, false},
		{"unknown flag safety", []string{"cargo", "add", "serde", "--mystery", "internal"}, false, 1, "serde", "", false, true},
		{"bare add", []string{"cargo", "add"}, false, 0, "", "", false, false},
		{"not add", []string{"cargo", "build"}, true, 0, "", "", false, false},
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
			if tt.wantCount > 0 {
				if got.Packages[0].Name != tt.wantName || got.Packages[0].Version != tt.wantVersion || got.Packages[0].Pinned != tt.wantPinned {
					t.Fatalf("first package = %#v, want name=%q version=%q pinned=%v", got.Packages[0], tt.wantName, tt.wantVersion, tt.wantPinned)
				}
			}
		})
	}
}
