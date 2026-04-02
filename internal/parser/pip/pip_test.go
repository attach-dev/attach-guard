package pip

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name         string
		command      []string
		wantNil      bool
		wantPM       string
		wantCount    int
		wantName     string
		wantVersion  string
		wantPinned   bool
		wantUnparsed bool
	}{
		{"basic", []string{"pip", "install", "requests"}, false, "pip", 1, "requests", "", false, false},
		{"pip3 pinned", []string{"pip3", "install", "requests==2.31.0"}, false, "pip3", 1, "requests", "2.31.0", true, false},
		{"bare install", []string{"pip", "install"}, false, "pip", 0, "", "", false, false},
		{"skip local path", []string{"pip", "install", "."}, false, "pip", 0, "", "", false, true},
		{"skip relative wheel path", []string{"pip", "install", "dist/pkg.whl"}, false, "pip", 0, "", "", false, true},
		{"skip file url", []string{"pip", "install", "file:///tmp/pkg.whl"}, false, "pip", 0, "", "", false, true},
		{"skip requirement file", []string{"pip", "install", "-r", "requirements.txt"}, false, "pip", 0, "", "", false, true},
		{"mixed parsed and skipped", []string{"pip", "install", ".", "flask"}, false, "pip", 1, "flask", "", false, true},
		{"range deferred", []string{"pip", "install", "requests>=2.0"}, false, "pip", 0, "", "", false, true},
		{"extras deferred", []string{"pip", "install", "requests[security]"}, false, "pip", 0, "", "", false, true},
		{"index url disqualifies public lookup", []string{"pip", "install", "flask", "--index-url", "https://custom.pypi.org/simple"}, false, "pip", 0, "", "", false, true},
		{"extra index disqualifies public lookup", []string{"pip", "install", "flask", "--extra-index-url", "https://custom.pypi.org/simple"}, false, "pip", 0, "", "", false, true},
		{"find links disqualifies public lookup", []string{"pip", "install", "flask", "--find-links", "https://example.com/simple"}, false, "pip", 0, "", "", false, true},
		{"known flag value not package", []string{"pip", "install", "flask", "--target", "/tmp"}, false, "pip", 1, "flask", "", false, false},
		{"unknown flag safety", []string{"pip", "install", "flask", "--mystery", "/tmp"}, false, "pip", 1, "flask", "", false, true},
		{"not install", []string{"pip", "--version"}, true, "", 0, "", "", false, false},
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
			if got.PackageManager != tt.wantPM {
				t.Fatalf("PackageManager = %q, want %q", got.PackageManager, tt.wantPM)
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
