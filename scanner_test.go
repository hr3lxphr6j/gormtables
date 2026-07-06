package main

import (
	"os"
	"testing"
)

// TestEmbedsBase verifies that embedsBase correctly identifies anonymous
// embedded types, both plain identifiers and selector expressions.
func TestEmbedsBase(t *testing.T) {
	src := `package p
type A struct {
	BaseModel
	Name string
}
type B struct {
	gorm.Model
	Name string
}
type C struct {
	name string
}
`
	models := parseModelsFromSrc(t, src, map[string]bool{"BaseModel": true, "gorm.Model": true})
	assertContains(t, models, "A")
	assertContains(t, models, "B")
	assertNotContains(t, models, "C")
}

// TestEnableMarker verifies that a struct without a base embed is included
// when it carries the enable marker comment.
func TestEnableMarker(t *testing.T) {
	src := `package p
// AutoMigrate:enable
type Standalone struct {
	Label string
}
`
	models := parseModelsFromSrc(t, src, nil)
	assertContains(t, models, "Standalone")
}

// TestDisableMarkerPrecedence verifies that the disable marker overrides the
// base embed, preventing the struct from being included.
func TestDisableMarkerPrecedence(t *testing.T) {
	src := `package p
// AutoMigrate:disable
type Excluded struct {
	BaseModel
}
`
	models := parseModelsFromSrc(t, src, map[string]bool{"BaseModel": true})
	assertNotContains(t, models, "Excluded")
}

// TestNonStructSkipped verifies that non-struct type declarations are ignored.
func TestNonStructSkipped(t *testing.T) {
	src := `package p
type MyInt int
type MyFunc func()
`
	models := parseModelsFromSrc(t, src, nil)
	if len(models) != 0 {
		t.Errorf("expected no models, got %v", modelNames(models))
	}
}

// TestBelongsToDepsSimple verifies that belongsToDeps correctly identifies a
// direct BelongsTo relationship.
func TestBelongsToDepsSimple(t *testing.T) {
	src := "package p\n" +
		"type Post struct {\n" +
		"\tBaseModel\n" +
		"\tUserID uint64\n" +
		"\tUser   *User `gorm:\"foreignKey:UserID\"`\n" +
		"}\n"

	models := parseModelsFromSrc(t, src, map[string]bool{"BaseModel": true})
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if len(models[0].deps) != 1 || models[0].deps[0] != "User" {
		t.Errorf("expected dep [User], got %v", models[0].deps)
	}
}

// TestBelongsToDepsHasManyExcluded verifies that a HasMany relationship (FK
// lives in the *other* struct) is not counted as a dependency of this struct.
func TestBelongsToDepsHasManyExcluded(t *testing.T) {
	src := "package p\n" +
		"type User struct {\n" +
		"\tBaseModel\n" +
		"\tPosts []Post `gorm:\"foreignKey:UserID\"`\n" +
		"}\n"
	// "UserID" is NOT a field of User, so this is a HasMany → no dep.
	models := parseModelsFromSrc(t, src, map[string]bool{"BaseModel": true})
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if len(models[0].deps) != 0 {
		t.Errorf("expected no deps for HasMany, got %v", models[0].deps)
	}
}

// TestBelongsToDepsNoSelfReference verifies that a self-referential BelongsTo
// (e.g. tree structures) is not added as a dependency.
func TestBelongsToDepsNoSelfReference(t *testing.T) {
	src := "package p\n" +
		"type Category struct {\n" +
		"\tBaseModel\n" +
		"\tParentID *uint64\n" +
		"\tParent   *Category `gorm:\"foreignKey:ParentID\"`\n" +
		"}\n"
	models := parseModelsFromSrc(t, src, map[string]bool{"BaseModel": true})
	if len(models[0].deps) != 0 {
		t.Errorf("expected no self-dep, got %v", models[0].deps)
	}
}

// TestTagBacktickStripping verifies that the backtick-stripping in
// belongsToDeps correctly parses struct tags from AST nodes even when the tag
// contains multiple semicolon-separated settings.
func TestTagBacktickStripping(t *testing.T) {
	src := "package p\n" +
		"type Order struct {\n" +
		"\tBaseModel\n" +
		"\tUserID uint64\n" +
		"\tUser   *User `gorm:\"foreignKey:UserID;references:ID\"`\n" +
		"}\n"
	models := parseModelsFromSrc(t, src, map[string]bool{"BaseModel": true})
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if len(models[0].deps) != 1 || models[0].deps[0] != "User" {
		t.Errorf("expected dep [User], got %v", models[0].deps)
	}
}

// ---- helpers ----

// parseModelsFromSrc is a test helper that writes src to a temp directory,
// then runs scanDirs and returns the discovered model entries.
func parseModelsFromSrc(t *testing.T, src string, bases map[string]bool) []modelEntry {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(dir+"/m.go", []byte(src), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	if err := os.WriteFile(dir+"/go.mod", []byte("module example.com/test\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	cfg := scanConfig{
		baseEmbeds:    bases,
		enableMarker:  "AutoMigrate:enable",
		disableMarker: "AutoMigrate:disable",
		tagKey:        "gorm",
		outputPkg:     "database",
		modulePath:    "example.com/test",
		moduleRoot:    dir,
	}

	models, _, err := scanDirs([]string{dir}, cfg)
	if err != nil {
		t.Fatalf("scanDirs: %v", err)
	}
	return models
}

func assertContains(t *testing.T, models []modelEntry, name string) {
	t.Helper()
	for _, m := range models {
		if m.TypeName == name {
			return
		}
	}
	t.Errorf("expected model %q in list %v", name, modelNames(models))
}

func assertNotContains(t *testing.T, models []modelEntry, name string) {
	t.Helper()
	for _, m := range models {
		if m.TypeName == name {
			t.Errorf("model %q should not be in list %v", name, modelNames(models))
			return
		}
	}
}

func modelNames(models []modelEntry) []string {
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.TypeName
	}
	return names
}
