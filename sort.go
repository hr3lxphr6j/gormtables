package main

import (
	"fmt"
	"sort"
	"strings"
)

// topoSort returns the model entries ordered so that, for every BelongsTo
// dependency A→B (A depends on B), A appears *before* B in the output slice.
// This mirrors how AutoMigrate must be called: child tables first so that
// foreign-key constraints can reference already-created parent tables.
//
// The ordering is fully deterministic: ties are broken by struct name so that
// identical inputs always produce identical outputs.
//
// If a cycle is detected the function returns an error listing the names of
// the structs involved.
func topoSort(models []modelEntry) ([]modelEntry, error) {
	// Build an index from struct name → entry.
	index := make(map[string]*modelEntry, len(models))
	for i := range models {
		index[models[i].TypeName] = &models[i]
	}

	// Filter deps: only keep references to models that are in the index.
	// Unknown references (types from external packages) are silently ignored
	// because they are not being managed by this run.
	deps := make(map[string][]string, len(models))
	for _, m := range models {
		var known []string
		for _, d := range m.deps {
			if _, ok := index[d]; ok {
				known = append(known, d)
			}
		}
		deps[m.TypeName] = known
	}

	// Compute in-degree for each node.
	inDegree := make(map[string]int, len(models))
	for _, m := range models {
		if _, ok := inDegree[m.TypeName]; !ok {
			inDegree[m.TypeName] = 0
		}
		for _, d := range deps[m.TypeName] {
			inDegree[d]++
		}
	}

	// Seed the queue with nodes that have no incoming edges (nothing depends
	// on them), sorted for determinism.
	queue := zeroInDegree(inDegree)

	var result []modelEntry
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, *index[name])

		// Reduce in-degree for each node that name depends on.
		for _, dep := range deps[name] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = insertSorted(queue, dep)
			}
		}
	}

	if len(result) != len(models) {
		// Some nodes were never dequeued → cycle.
		var cycle []string
		for name, deg := range inDegree {
			if deg > 0 {
				cycle = append(cycle, name)
			}
		}
		sort.Strings(cycle)
		return nil, fmt.Errorf("dependency cycle detected among: %s", strings.Join(cycle, ", "))
	}

	return result, nil
}

// zeroInDegree returns a sorted list of names whose in-degree is zero.
func zeroInDegree(inDegree map[string]int) []string {
	var names []string
	for name, deg := range inDegree {
		if deg == 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// insertSorted inserts name into the already-sorted slice s, maintaining
// ascending lexicographic order.
func insertSorted(s []string, name string) []string {
	i := sort.SearchStrings(s, name)
	s = append(s, "")
	copy(s[i+1:], s[i:])
	s[i] = name
	return s
}
