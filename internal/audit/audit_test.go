package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestLogger_Log(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger := NewLogger(path)
	err := logger.Log(Entry{
		PackageManager:  "npm",
		OriginalCommand: "npm install axios",
		Decision:        api.Allow,
		Reason:          "passes policy",
		Provider:        "mock",
		Mode:            "shell",
		Packages: []api.PackageEvaluation{
			{
				Ecosystem:       api.EcosystemNPM,
				Name:            "axios",
				SelectedVersion: "1.7.0",
				Score:           api.PackageScore{SupplyChain: 92, Overall: 88},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Read and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}

	if entry.Decision != api.Allow {
		t.Errorf("expected Allow, got %s", entry.Decision)
	}
	if entry.OriginalCommand != "npm install axios" {
		t.Errorf("unexpected command: %s", entry.OriginalCommand)
	}
	if entry.Timestamp == "" {
		t.Error("expected timestamp")
	}
	if entry.User == "" {
		t.Error("expected user")
	}
	if len(entry.Packages) != 1 {
		t.Errorf("expected 1 package, got %d", len(entry.Packages))
	}
}

func TestLogger_MultipleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	logger := NewLogger(path)
	for i := 0; i < 3; i++ {
		err := logger.Log(Entry{
			PackageManager:  "npm",
			OriginalCommand: "npm install test",
			Decision:        api.Allow,
			Reason:          "ok",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected 3 log lines, got %d", lines)
	}
}
