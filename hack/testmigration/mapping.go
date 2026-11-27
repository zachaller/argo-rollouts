package main

import (
	"fmt"
	"os"
	"sort"

	"sigs.k8s.io/yaml"
)

const (
	kindUnchanged = "unchanged"
	kindConverted = "converted"
	kindRenamed   = "renamed"
)

// pair links a baseline test to the test that replaces it at HEAD.
type pair struct {
	Base test
	Head test
	Kind string
}

// mapping is the result of matching every baseline test to HEAD.
type mapping struct {
	Pairs []pair
	// Missing are baseline tests with no counterpart at HEAD.
	Missing []test
	// Duplicated are baseline tests that exist at HEAD both in their old and
	// in their converted form.
	Duplicated []test
	// Collisions are HEAD tests claimed by more than one baseline test,
	// keyed by HEAD test id.
	Collisions map[string][]test
	// Added are HEAD tests that no baseline test maps to.
	Added []test
	// StaleRenames are rename entries whose baseline test does not exist.
	StaleRenames []string
}

func (m mapping) ok() bool {
	return len(m.Missing) == 0 && len(m.Duplicated) == 0 && len(m.Collisions) == 0 && len(m.StaleRenames) == 0
}

// loadStringMap reads a flat YAML map of strings. A missing file is an empty map.
func loadStringMap(path string) (map[string]string, error) {
	out := map[string]string{}
	if path == "" {
		return out, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if out == nil {
		out = map[string]string{}
	}
	return out, nil
}

// renameFor looks up a rename by "dir:name" first and then by bare name.
func renameFor(renames map[string]string, t test) (string, bool) {
	if r, ok := renames[t.id()]; ok {
		return r, true
	}
	r, ok := renames[t.name()]
	return r, ok
}

// mapTests matches every baseline test to exactly one HEAD test.
//
// A baseline test may stay as it is (same name), or a suite method
// "TestXSuite/TestY" may become the plain function "TestY". Any other target
// name has to be listed in renames (baseline name -> HEAD name).
func mapTests(base, head inventory, renames map[string]string) mapping {
	m := mapping{Collisions: map[string][]test{}}
	claimed := map[string][]test{}
	usedRenames := map[string]bool{}

	headByDir := map[string]map[string]test{}
	for _, t := range head.Tests {
		if headByDir[t.Dir] == nil {
			headByDir[t.Dir] = map[string]test{}
		}
		headByDir[t.Dir][t.name()] = t
	}

	for _, b := range base.Tests {
		headByName := headByDir[b.Dir]
		var targets []pair

		if h, ok := headByName[b.name()]; ok {
			targets = append(targets, pair{Base: b, Head: h, Kind: kindUnchanged})
		}

		want, kind := "", kindConverted
		if r, ok := renameFor(renames, b); ok {
			want, kind = r, kindRenamed
			if _, ok := renames[b.id()]; ok {
				usedRenames[b.id()] = true
			} else {
				usedRenames[b.name()] = true
			}
		} else if b.Suite != "" {
			want = b.Name
		}
		if want != "" && want != b.name() {
			if h, ok := headByName[want]; ok {
				targets = append(targets, pair{Base: b, Head: h, Kind: kind})
			}
		}

		switch len(targets) {
		case 0:
			m.Missing = append(m.Missing, b)
		case 1:
			m.Pairs = append(m.Pairs, targets[0])
			claimed[targets[0].Head.id()] = append(claimed[targets[0].Head.id()], b)
		default:
			m.Duplicated = append(m.Duplicated, b)
		}
	}

	for id, bases := range claimed {
		if len(bases) > 1 {
			m.Collisions[id] = bases
		}
	}
	for _, h := range head.Tests {
		if len(claimed[h.id()]) == 0 {
			m.Added = append(m.Added, h)
		}
	}
	for key := range renames {
		if !usedRenames[key] {
			m.StaleRenames = append(m.StaleRenames, key)
		}
	}
	sort.Strings(m.StaleRenames)
	return m
}

func (m mapping) counts() map[string]int {
	c := map[string]int{}
	for _, p := range m.Pairs {
		c[p.Kind]++
	}
	return c
}
