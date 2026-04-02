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
		wantNonLocal bool
	}{
		{"basic", []string{"pip", "install", "requests"}, false, "pip", 1, "requests", "", false, false, false},
		{"pip3 pinned", []string{"pip3", "install", "requests==2.31.0"}, false, "pip3", 1, "requests", "2.31.0", true, false, false},
		{"bare install", []string{"pip", "install"}, false, "pip", 0, "", "", false, false, false},
		{"unknown pre action flag safety", []string{"pip", "--mystery", "/tmp", "install", "flask"}, false, "pip", 0, "", "", false, true, true},
		{"pre action proxy", []string{"pip", "--proxy", "http://proxy.example", "install", "flask"}, false, "pip", 0, "", "", false, true, true},
		{"post action index assignment", []string{"pip", "install", "flask", "--index-url=https://custom.pypi.org/simple"}, false, "pip", 0, "", "", false, true, true},
		{"post action requirement assignment", []string{"pip", "install", "--requirement=requirements.txt"}, false, "pip", 0, "", "", false, true, true},
		{"skip local path", []string{"pip", "install", "."}, false, "pip", 0, "", "", false, true, false},
		{"skip relative wheel path", []string{"pip", "install", "dist/pkg.whl"}, false, "pip", 0, "", "", false, true, false},
		{"skip file url", []string{"pip", "install", "file:///tmp/pkg.whl"}, false, "pip", 0, "", "", false, true, false},
		{"find links local path", []string{"pip", "install", "--find-links", "./dist", "flask"}, false, "pip", 0, "", "", false, true, true},
		{"skip remote vcs url", []string{"pip", "install", "git+https://github.com/user/repo.git"}, false, "pip", 0, "", "", false, true, true},
		{"skip requirement file", []string{"pip", "install", "-r", "requirements.txt"}, false, "pip", 0, "", "", false, true, true},
		{"mixed parsed and skipped", []string{"pip", "install", ".", "flask"}, false, "pip", 1, "flask", "", false, true, false},
		{"upgrade boolean flag", []string{"pip", "install", "--upgrade", "flask"}, false, "pip", 1, "flask", "", false, false, false},
		{"short upgrade boolean flag", []string{"pip", "install", "-U", "flask"}, false, "pip", 1, "flask", "", false, false, false},
		{"range deferred", []string{"pip", "install", "requests>=2.0"}, false, "pip", 0, "", "", false, true, true},
		{"extras deferred", []string{"pip", "install", "requests[security]"}, false, "pip", 0, "", "", false, true, true},
		{"index url disqualifies public lookup", []string{"pip", "install", "flask", "--index-url", "https://custom.pypi.org/simple"}, false, "pip", 0, "", "", false, true, true},
		{"extra index disqualifies public lookup", []string{"pip", "install", "flask", "--extra-index-url", "https://custom.pypi.org/simple"}, false, "pip", 0, "", "", false, true, true},
		{"find links disqualifies public lookup", []string{"pip", "install", "flask", "--find-links", "https://example.com/simple"}, false, "pip", 0, "", "", false, true, true},
		{"known flag value not package", []string{"pip", "install", "flask", "--target", "/tmp"}, false, "pip", 1, "flask", "", false, false, false},
		{"known flag assignment not package", []string{"pip", "install", "flask", "--target=/tmp"}, false, "pip", 1, "flask", "", false, false, false},
		{"unknown flag safety", []string{"pip", "install", "flask", "--mystery", "/tmp"}, false, "pip", 0, "", "", false, true, true},
		{"unknown flag assignment safety", []string{"pip", "install", "flask", "--mystery=/tmp"}, false, "pip", 0, "", "", false, true, true},
		{"not install", []string{"pip", "--version"}, true, "", 0, "", "", false, false, false},
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
