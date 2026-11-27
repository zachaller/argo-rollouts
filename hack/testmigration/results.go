package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	statusPass   = "pass"
	statusFail   = "fail"
	statusSkip   = "skip"
	statusNotRun = "not-run"
)

// result is the final outcome of one test name across all of its attempts
// (gotestsum writes one entry per attempt when it reruns failures).
type result struct {
	Status   string
	Attempts int
	Failures int
}

func (r result) flaky() bool { return r.Status == statusPass && r.Failures > 0 }

type xmlCase struct {
	Name    string    `xml:"name,attr"`
	Failure *struct{} `xml:"failure"`
	Error   *struct{} `xml:"error"`
	Skipped *struct{} `xml:"skipped"`
}

type xmlSuite struct {
	Cases []xmlCase `xml:"testcase"`
}

// xmlRoot accepts both a <testsuites> root and a bare <testsuite> root.
type xmlRoot struct {
	Suites []xmlSuite `xml:"testsuite"`
	Cases  []xmlCase  `xml:"testcase"`
}

// parseJUnit reads a JUnit file and returns the final result per test name.
func parseJUnit(data []byte) (map[string]result, error) {
	var root xmlRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	cases := append([]xmlCase{}, root.Cases...)
	for _, s := range root.Suites {
		cases = append(cases, s.Cases...)
	}

	type tally struct{ attempts, passes, failures, skips int }
	tallies := map[string]*tally{}
	for _, c := range cases {
		t := tallies[c.Name]
		if t == nil {
			t = &tally{}
			tallies[c.Name] = t
		}
		t.attempts++
		switch {
		case c.Failure != nil || c.Error != nil:
			t.failures++
		case c.Skipped != nil:
			t.skips++
		default:
			t.passes++
		}
	}

	out := map[string]result{}
	for name, t := range tallies {
		r := result{Attempts: t.attempts, Failures: t.failures}
		switch {
		case t.passes > 0:
			r.Status = statusPass
		case t.failures > 0:
			r.Status = statusFail
		default:
			r.Status = statusSkip
		}
		out[name] = r
	}
	return out, nil
}

func parseJUnitFile(file string) (map[string]result, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	results, err := parseJUnit(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", file, err)
	}
	return results, nil
}

// statusOf returns the result of an inventory test. A suite method with no
// entry of its own inherits a skip or failure of its suite runner, which is
// how a suite that skips or fails in SetupSuite is reported.
func statusOf(results map[string]result, t test) result {
	if r, ok := results[t.name()]; ok {
		return r
	}
	if t.Suite != "" {
		if r, ok := results[t.Runner]; ok && (r.Status == statusSkip || r.Status == statusFail) {
			return r
		}
	}
	return result{Status: statusNotRun}
}

// loadPatterns reads one glob pattern per line; blank lines and # comments are ignored.
func loadPatterns(file string) ([]string, error) {
	if file == "" {
		return nil, nil
	}
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := path.Match(line, ""); err != nil {
			return nil, fmt.Errorf("%s: bad pattern %q: %w", file, line, err)
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

func matchAny(patterns []string, name string) (string, bool) {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return p, true
		}
	}
	return "", false
}

// junitReport compares the outcome of every mapped test before and after.
type junitReport struct {
	Compared int
	// Failures of the check.
	Regressions     []string // passed or skipped before, fails now
	NewlySkipped    []string // passed before, skipped now
	UnexpectedSkips []string // skipped now and not on the expected-skip list
	NotRun          []string // has no result now
	// Information only.
	NowRuns            []string // skipped before, passes now
	Flaky              []string // passed now only after a failed attempt
	BaseFailures       []string // failed in the baseline
	StaleExpectedSkips []string // expected-skip patterns that matched nothing
}

func (r junitReport) ok() bool {
	return len(r.Regressions) == 0 && len(r.NewlySkipped) == 0 && len(r.UnexpectedSkips) == 0 && len(r.NotRun) == 0
}

// compareJUnit checks every mapped pair. only, when set, restricts the check
// to HEAD test names it matches.
func compareJUnit(m mapping, baseResults, newResults map[string]result, expectedSkips []string, only *regexp.Regexp) junitReport {
	var rep junitReport
	usedPatterns := map[string]bool{}

	for _, p := range m.Pairs {
		name := p.Head.name()
		if only != nil && !only.MatchString(name) {
			continue
		}
		rep.Compared++
		before := statusOf(baseResults, p.Base)
		after := statusOf(newResults, p.Head)

		if before.Status == statusFail {
			rep.BaseFailures = append(rep.BaseFailures, name)
		}
		switch after.Status {
		case statusNotRun:
			rep.NotRun = append(rep.NotRun, name)
		case statusFail:
			rep.Regressions = append(rep.Regressions, name)
		case statusSkip:
			if pattern, ok := matchAny(expectedSkips, name); ok {
				usedPatterns[pattern] = true
			} else {
				rep.UnexpectedSkips = append(rep.UnexpectedSkips, name)
			}
			if before.Status == statusPass {
				rep.NewlySkipped = append(rep.NewlySkipped, name)
			}
		case statusPass:
			if before.Status == statusSkip {
				rep.NowRuns = append(rep.NowRuns, name)
			}
			if after.flaky() {
				rep.Flaky = append(rep.Flaky, fmt.Sprintf("%s (%d failed attempts)", name, after.Failures))
			}
		}
	}

	if only == nil {
		for _, pattern := range expectedSkips {
			if !usedPatterns[pattern] {
				rep.StaleExpectedSkips = append(rep.StaleExpectedSkips, pattern)
			}
		}
	}
	for _, list := range [][]string{rep.Regressions, rep.NewlySkipped, rep.UnexpectedSkips, rep.NotRun, rep.NowRuns, rep.Flaky, rep.BaseFailures} {
		sort.Strings(list)
	}
	return rep
}

// killFinding is the comparison of one seeded bug before and after.
type killFinding struct {
	Bug string
	// Lost are tests that failed with the bug in the baseline and pass now.
	Lost []string
	// NotRun are tests that failed with the bug in the baseline and have no result now.
	NotRun []string
	// Gained are tests that passed in the baseline and fail now.
	Gained []string
	// Caught is how many compared tests fail with the bug in the baseline.
	Caught int
	// Missing is set when the new directory has no result file for this bug.
	Missing bool
}

func (f killFinding) ok() bool { return !f.Missing && len(f.Lost) == 0 && len(f.NotRun) == 0 }

// compareKills compares, for every seeded bug, which tests fail. baseDir and
// newDir each hold one JUnit file per bug, named <bug>.xml.
func compareKills(m mapping, baseDir, newDir string, only *regexp.Regexp) ([]killFinding, error) {
	files, err := filepath.Glob(filepath.Join(baseDir, "*.xml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var findings []killFinding
	for _, baseFile := range files {
		bug := strings.TrimSuffix(filepath.Base(baseFile), ".xml")
		f := killFinding{Bug: bug}
		baseResults, err := parseJUnitFile(baseFile)
		if err != nil {
			return nil, err
		}
		newResults, err := parseJUnitFile(filepath.Join(newDir, bug+".xml"))
		if os.IsNotExist(err) {
			f.Missing = true
			findings = append(findings, f)
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, p := range m.Pairs {
			name := p.Head.name()
			if only != nil && !only.MatchString(name) {
				continue
			}
			before := statusOf(baseResults, p.Base).Status
			after := statusOf(newResults, p.Head).Status
			if before == statusFail {
				f.Caught++
				switch after {
				case statusFail:
				case statusNotRun:
					f.NotRun = append(f.NotRun, name)
				default:
					f.Lost = append(f.Lost, name)
				}
			} else if before == statusPass && after == statusFail {
				f.Gained = append(f.Gained, name)
			}
		}
		sort.Strings(f.Lost)
		sort.Strings(f.NotRun)
		sort.Strings(f.Gained)
		findings = append(findings, f)
	}
	return findings, nil
}
