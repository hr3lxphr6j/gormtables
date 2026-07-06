package main

import (
	"strings"
	"testing"
)

// entry is a helper to build a modelEntry inline for tests.
func entry(name string, deps ...string) modelEntry {
	return modelEntry{TypeName: name, deps: deps}
}

// TestTopoSortLinear verifies a simple linear dependency chain: C → B → A
// (C depends on B which depends on A). Expected output: C, B, A.
func TestTopoSortLinear(t *testing.T) {
	models := []modelEntry{
		entry("A"),
		entry("B", "A"),
		entry("C", "B"),
	}
	sorted, err := topoSort(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertOrder(t, sorted, "C", "B", "A")
}

// TestTopoSortDiamond verifies a diamond dependency: C and D both depend on B,
// B depends on A.  Both valid orderings of C/D are acceptable, but A must
// be last.
func TestTopoSortDiamond(t *testing.T) {
	models := []modelEntry{
		entry("A"),
		entry("B", "A"),
		entry("C", "B"),
		entry("D", "B"),
	}
	sorted, err := topoSort(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBefore(t, sorted, "C", "B")
	assertBefore(t, sorted, "D", "B")
	assertBefore(t, sorted, "B", "A")
}

// TestTopoSortDeterministic verifies that running topoSort twice on the same
// input (possibly different slice order) produces identical results.
func TestTopoSortDeterministic(t *testing.T) {
	models1 := []modelEntry{entry("X"), entry("Y", "X"), entry("Z", "X")}
	models2 := []modelEntry{entry("Z", "X"), entry("X"), entry("Y", "X")}

	sorted1, _ := topoSort(models1)
	sorted2, _ := topoSort(models2)

	if len(sorted1) != len(sorted2) {
		t.Fatalf("length mismatch: %d vs %d", len(sorted1), len(sorted2))
	}
	for i := range sorted1 {
		if sorted1[i].TypeName != sorted2[i].TypeName {
			t.Errorf("position %d: %s vs %s", i, sorted1[i].TypeName, sorted2[i].TypeName)
		}
	}
}

// TestTopoSortCycleDetected verifies that a cycle causes topoSort to return a
// non-nil error mentioning the involved nodes.
func TestTopoSortCycleDetected(t *testing.T) {
	models := []modelEntry{
		entry("A", "B"),
		entry("B", "A"),
	}
	_, err := topoSort(models)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "A") || !strings.Contains(err.Error(), "B") {
		t.Errorf("error should mention cycle nodes, got: %v", err)
	}
}

// TestTopoSortNoModels verifies that an empty input is handled gracefully.
func TestTopoSortNoModels(t *testing.T) {
	sorted, err := topoSort(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 0 {
		t.Errorf("expected empty result, got %v", sorted)
	}
}

// TestTopoSortUnknownDepIgnored verifies that deps that are not in the input
// set are silently dropped (they belong to external packages).
func TestTopoSortUnknownDepIgnored(t *testing.T) {
	models := []modelEntry{
		entry("A", "ExternalModel"),
	}
	sorted, err := topoSort(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 1 || sorted[0].TypeName != "A" {
		t.Errorf("expected [A], got %v", modelNames(sorted))
	}
}

// ---- helpers ----

// assertOrder checks that the sorted slice contains exactly the given names in
// that exact order.
func assertOrder(t *testing.T, sorted []modelEntry, names ...string) {
	t.Helper()
	got := modelNames(sorted)
	if len(got) != len(names) {
		t.Fatalf("expected %v, got %v", names, got)
	}
	for i, name := range names {
		if got[i] != name {
			t.Errorf("position %d: want %s, got %s", i, name, got[i])
		}
	}
}

// assertBefore checks that a appears somewhere before b in the sorted slice.
func assertBefore(t *testing.T, sorted []modelEntry, a, b string) {
	t.Helper()
	ai, bi := -1, -1
	for i, m := range sorted {
		if m.TypeName == a {
			ai = i
		}
		if m.TypeName == b {
			bi = i
		}
	}
	if ai == -1 {
		t.Errorf("%s not found in sorted list", a)
		return
	}
	if bi == -1 {
		t.Errorf("%s not found in sorted list", b)
		return
	}
	if ai >= bi {
		t.Errorf("expected %s before %s, but %s is at %d, %s at %d", a, b, a, ai, b, bi)
	}
}
