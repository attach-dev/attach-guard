package gomod

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
		{"basic", []string{"go", "get", "golang.org/x/net"}, false, 1, "golang.org/x/net", "", false, false, false},
		{"latest", []string{"go", "get", "golang.org/x/net@latest"}, false, 1, "golang.org/x/net", "", false, false, false},
		{"exact semver", []string{"go", "get", "golang.org/x/net@v0.25.0"}, false, 1, "golang.org/x/net", "v0.25.0", true, false, false},
		{"local path deferred", []string{"go", "get", "./..."}, false, 0, "", "", false, true, false},
		{"current module deferred", []string{"go", "get", "."}, false, 0, "", "", false, true, false},
		{"parent module deferred", []string{"go", "get", ".."}, false, 0, "", "", false, true, false},
		{"query deferred", []string{"go", "get", "golang.org/x/net@upgrade"}, false, 0, "", "", false, true, true},
		{"prefix deferred", []string{"go", "get", "golang.org/x/net@v0.3"}, false, 0, "", "", false, true, true},
		{"bare get", []string{"go", "get"}, false, 0, "", "", false, false, false},
		{"not get", []string{"go", "build", "./..."}, true, 0, "", "", false, false, false},
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
