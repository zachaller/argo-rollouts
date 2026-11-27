package main

import (
	"fmt"
	"sort"
)

// bodyFinding is one test whose body at HEAD is not what the baseline had.
type bodyFinding struct {
	Pair pair
	// Problem is a structural problem with the conversion; Diff is a text diff
	// of the bodies. Either may be empty, but not both.
	Problem string
	Diff    string
	// Reason is set when the difference is listed in the exceptions file.
	Reason string
}

func (f bodyFinding) accepted() bool { return f.Reason != "" }

// compareBodies checks every mapped pair.
//
//   - A test that kept its name must have an identical body.
//   - A suite method converted to a plain function must start with a harness
//     constructor that assigns the variable the old receiver used, and the
//     rest of the body must be identical to the old method body. Options
//     passed to the constructor count as a difference.
//
// exceptions maps a HEAD test name to the reason its difference is accepted.
// The second return value lists exception entries that no longer apply.
func compareBodies(m mapping, exceptions map[string]string) ([]bodyFinding, []string) {
	var findings []bodyFinding
	differing := map[string]bool{}

	for _, p := range m.Pairs {
		f := bodyFinding{Pair: p}
		baseBody, headBody := p.Base.Body, p.Head.Body

		converted := p.Base.Suite != "" && p.Head.Suite == ""
		if converted {
			switch {
			case p.Head.CtorVar == "":
				f.Problem = fmt.Sprintf("first statement is not a harness constructor: %q", p.Head.First)
			case p.Head.CtorVar != p.Base.Receiver:
				f.Problem = fmt.Sprintf("constructor assigns %q but the suite method used receiver %q", p.Head.CtorVar, p.Base.Receiver)
			default:
				headBody = p.Head.Rest
				// The constructor line is not part of the compared body, but
				// options on it change how the test runs, so they count as a
				// difference that needs a listed reason.
				if p.Head.CtorOptions != "" {
					f.Problem = fmt.Sprintf("the constructor is called with options: %s", p.Head.CtorOptions)
				}
			}
		}

		f.Diff = lineDiff(baseBody, headBody, 2)
		if f.Problem == "" && f.Diff == "" {
			continue
		}
		name := p.Head.name()
		differing[name] = true
		if reason, ok := exceptions[name]; ok {
			f.Reason = reason
		}
		findings = append(findings, f)
	}

	var stale []string
	for name := range exceptions {
		if !differing[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	sort.Slice(findings, func(i, j int) bool { return findings[i].Pair.Head.id() < findings[j].Pair.Head.id() })
	return findings, stale
}
