package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestEndToEnd runs the full pipeline (scan → sort → generate) against the
// testdata/models fixture package and compares the result against the golden
// file testdata/golden.go.
//
// To update the golden file, run:
//
//	go run . -models testdata/models -out testdata/golden.go -pkg database
func TestEndToEnd(t *testing.T) {
	// Locate the module root (the directory containing this test file, which
	// is also the module root of gormtables itself).
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	modelsDir := filepath.Join(wd, "testdata", "models")
	goldenFile := filepath.Join(wd, "testdata", "golden.go")
	outFile := filepath.Join(t.TempDir(), "tables.go")

	if err := run(config{
		modelDirs:     []string{modelsDir},
		outFile:       outFile,
		outputPkg:     "database",
		varName:       "autoMigrateTables",
		baseEmbeds:    []string{"BaseModel", "gorm.Model"},
		enableMarker:  "AutoMigrate:enable",
		disableMarker: "AutoMigrate:disable",
		tagKey:        "gorm",
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	// When the golden file does not yet exist, create it.
	if _, statErr := os.Stat(goldenFile); os.IsNotExist(statErr) {
		if writeErr := os.WriteFile(goldenFile, got, 0644); writeErr != nil {
			t.Fatalf("writing golden file: %v", writeErr)
		}
		t.Logf("golden file created at %s", goldenFile)
		return
	}

	golden, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if !bytes.Equal(got, golden) {
		t.Errorf("output does not match golden file.\n\n--- want (golden) ---\n%s\n--- got ---\n%s",
			golden, got)
	}
}
