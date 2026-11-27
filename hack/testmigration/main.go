// Command testmigration checks that the e2e test migration does not lose or
// silently change tests. See README.md in this directory.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type dirList []string

func (d *dirList) String() string     { return strings.Join(*d, ",") }
func (d *dirList) Set(v string) error { *d = append(*d, v); return nil }

// common holds the flags shared by the subcommands.
type common struct {
	fs      *flag.FlagSet
	base    string
	head    string
	dirs    dirList
	renames string
}

func newCommon(name string, needBase bool) *common {
	c := &common{fs: flag.NewFlagSet(name, flag.ExitOnError)}
	if needBase {
		c.fs.StringVar(&c.base, "base", "", "git ref of the baseline (required)")
		c.fs.StringVar(&c.head, "head", "", "git ref to check (default: the working tree)")
		c.fs.StringVar(&c.renames, "renames", "test/e2e/renames.yaml", "YAML map of baseline test name to new test name")
	}
	c.fs.Var(&c.dirs, "dir", "directory to scan for tests; repeatable (default test/e2e)")
	return c
}

func (c *common) parse(args []string) {
	_ = c.fs.Parse(args)
	if len(c.dirs) == 0 {
		c.dirs = dirList{"test/e2e"}
	}
}

func (c *common) inventoryAt(ref string) (inventory, error) {
	files, err := loadTestFiles(ref, c.dirs)
	if err != nil {
		return inventory{}, err
	}
	return buildInventory(files)
}

// mapping builds the baseline-to-HEAD mapping from the common flags.
func (c *common) mapping() (mapping, error) {
	if c.base == "" {
		return mapping{}, fmt.Errorf("-base is required")
	}
	base, err := c.inventoryAt(c.base)
	if err != nil {
		return mapping{}, err
	}
	head, err := c.inventoryAt(c.head)
	if err != nil {
		return mapping{}, err
	}
	renames, err := loadStringMap(c.renames)
	if err != nil {
		return mapping{}, err
	}
	return mapTests(base, head, renames), nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var (
		ok  bool
		err error
	)
	args := os.Args[2:]
	switch os.Args[1] {
	case "inventory":
		ok, err = cmdInventory(os.Stdout, args)
	case "inventory-diff":
		ok, err = cmdInventoryDiff(os.Stdout, args)
	case "bodydiff":
		ok, err = cmdBodyDiff(os.Stdout, args)
	case "junitdiff":
		ok, err = cmdJUnitDiff(os.Stdout, args)
	case "killmatrix":
		ok, err = cmdKillMatrix(os.Stdout, args)
	case "skips":
		ok, err = cmdSkips(os.Stdout, args)
	case "convert":
		ok, err = cmdConvert(os.Stdout, args)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	if !ok {
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: go run ./hack/testmigration <command> [flags]

commands:
  inventory       list every test in the scanned directories
  inventory-diff  fail unless every baseline test maps to exactly one test now
  bodydiff        fail if a test body differs from the baseline without a listed reason
  junitdiff       compare pass/fail/skip results before and after
  killmatrix      compare which tests catch each seeded bug before and after
  skips           fail if a run skipped a test that is not on the expected-skip list
  convert         rewrite one testify suite into plain test functions

Run a command with -h for its flags.
`)
}

func cmdInventory(w io.Writer, args []string) (bool, error) {
	c := newCommon("inventory", false)
	ref := c.fs.String("ref", "", "git ref to read (default: the working tree)")
	subtests := c.fs.Bool("subtests", false, "also list static subtest names")
	c.parse(args)

	inv, err := c.inventoryAt(*ref)
	if err != nil {
		return false, err
	}
	for _, t := range inv.Tests {
		fmt.Fprintf(w, "%s\t%s:%d\n", t.id(), t.File, t.Line)
		if *subtests {
			for _, sub := range t.Subtests {
				fmt.Fprintf(w, "%s/%s\t%s:%d\n", t.id(), sub, t.File, t.Line)
			}
		}
	}
	for _, t := range inv.Orphans {
		fmt.Fprintf(w, "# never runs (no suite runner): %s.%s\t%s:%d\n", t.Suite, t.Name, t.File, t.Line)
	}
	for _, r := range inv.EmptyRunners {
		fmt.Fprintf(w, "# suite runner with no tests left: %s\n", r)
	}
	fmt.Fprintf(w, "# %d tests\n", len(inv.Tests))
	return true, nil
}

func cmdInventoryDiff(w io.Writer, args []string) (bool, error) {
	c := newCommon("inventory-diff", true)
	failOnAdded := c.fs.Bool("fail-on-added", false, "also fail when HEAD has tests the baseline did not")
	c.parse(args)

	m, err := c.mapping()
	if err != nil {
		return false, err
	}
	counts := m.counts()
	fmt.Fprintf(w, "baseline tests: %d  unchanged: %d  converted: %d  renamed: %d\n",
		len(m.Pairs)+len(m.Missing)+len(m.Duplicated), counts[kindUnchanged], counts[kindConverted], counts[kindRenamed])

	for _, t := range m.Missing {
		fmt.Fprintf(w, "MISSING     %s (%s:%d) has no counterpart\n", t.id(), t.File, t.Line)
	}
	for _, t := range m.Duplicated {
		fmt.Fprintf(w, "DUPLICATED  %s exists both in its old and its converted form\n", t.id())
	}
	ids := make([]string, 0, len(m.Collisions))
	for id := range m.Collisions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var names []string
		for _, b := range m.Collisions[id] {
			names = append(names, b.name())
		}
		fmt.Fprintf(w, "COLLISION   %s is the target of %s; add an entry to the renames file\n", id, strings.Join(names, " and "))
	}
	for _, key := range m.StaleRenames {
		fmt.Fprintf(w, "STALE       rename %q names a test the baseline does not have\n", key)
	}
	for _, t := range m.Added {
		fmt.Fprintf(w, "added       %s (%s:%d)\n", t.id(), t.File, t.Line)
	}

	ok := m.ok() && !(*failOnAdded && len(m.Added) > 0)
	if ok {
		fmt.Fprintln(w, "OK: every baseline test maps to exactly one test")
	}
	return ok, nil
}

func cmdBodyDiff(w io.Writer, args []string) (bool, error) {
	c := newCommon("bodydiff", true)
	exceptionsFile := c.fs.String("exceptions", "test/e2e/body-exceptions.yaml", "YAML map of test name to the reason its body may differ")
	verbose := c.fs.Bool("v", false, "also print the diff of accepted exceptions")
	c.parse(args)

	m, err := c.mapping()
	if err != nil {
		return false, err
	}
	exceptions, err := loadStringMap(*exceptionsFile)
	if err != nil {
		return false, err
	}
	findings, stale := compareBodies(m, exceptions)

	unaccepted := 0
	for _, f := range findings {
		label := "DIFFERS "
		if f.accepted() {
			label = "accepted"
		} else {
			unaccepted++
		}
		fmt.Fprintf(w, "%s %s (%s:%d)\n", label, f.Pair.Head.name(), f.Pair.Head.File, f.Pair.Head.Line)
		if f.accepted() {
			fmt.Fprintf(w, "         reason: %s\n", f.Reason)
		}
		if f.Problem != "" {
			fmt.Fprintf(w, "         %s\n", f.Problem)
		}
		if f.Diff != "" && (!f.accepted() || *verbose) {
			for _, line := range strings.Split(strings.TrimRight(f.Diff, "\n"), "\n") {
				fmt.Fprintf(w, "         %s\n", line)
			}
		}
	}
	for _, name := range stale {
		fmt.Fprintf(w, "STALE    exception %q no longer differs from the baseline; remove it\n", name)
	}

	counts := m.counts()
	fmt.Fprintf(w, "compared %d tests (%d converted or renamed): %d identical, %d accepted exceptions, %d unexplained\n",
		len(m.Pairs), counts[kindConverted]+counts[kindRenamed], len(m.Pairs)-len(findings), len(findings)-unaccepted, unaccepted)
	return unaccepted == 0 && len(stale) == 0, nil
}

func compileOnly(expr string) (*regexp.Regexp, error) {
	if expr == "" {
		return nil, nil
	}
	return regexp.Compile(expr)
}

func printList(w io.Writer, label string, items []string) {
	for _, item := range items {
		fmt.Fprintf(w, "%-16s %s\n", label, item)
	}
}

func cmdJUnitDiff(w io.Writer, args []string) (bool, error) {
	c := newCommon("junitdiff", true)
	baseJUnit := c.fs.String("base-junit", "", "JUnit file from the baseline run (required)")
	newJUnit := c.fs.String("new-junit", "", "JUnit file from the run to check (required)")
	skipsFile := c.fs.String("expected-skips", "test/e2e/expected-skips.txt", "glob patterns of tests that are allowed to skip")
	onlyExpr := c.fs.String("only", "", "regular expression; only check tests whose new name matches")
	c.parse(args)
	if *baseJUnit == "" || *newJUnit == "" {
		return false, fmt.Errorf("-base-junit and -new-junit are required")
	}

	m, err := c.mapping()
	if err != nil {
		return false, err
	}
	only, err := compileOnly(*onlyExpr)
	if err != nil {
		return false, err
	}
	baseResults, err := parseJUnitFile(*baseJUnit)
	if err != nil {
		return false, err
	}
	newResults, err := parseJUnitFile(*newJUnit)
	if err != nil {
		return false, err
	}
	expectedSkips, err := loadPatterns(*skipsFile)
	if err != nil {
		return false, err
	}

	rep := compareJUnit(m, baseResults, newResults, expectedSkips, only)
	printList(w, "REGRESSION", rep.Regressions)
	printList(w, "NEWLY SKIPPED", rep.NewlySkipped)
	printList(w, "UNEXPECTED SKIP", rep.UnexpectedSkips)
	printList(w, "NOT RUN", rep.NotRun)
	printList(w, "now runs", rep.NowRuns)
	printList(w, "flaky", rep.Flaky)
	printList(w, "failed in base", rep.BaseFailures)
	printList(w, "stale skip entry", rep.StaleExpectedSkips)
	fmt.Fprintf(w, "compared %d tests\n", rep.Compared)
	if rep.ok() {
		fmt.Fprintln(w, "OK: no test regressed, newly skipped, or went missing")
	}
	return rep.ok(), nil
}

func cmdKillMatrix(w io.Writer, args []string) (bool, error) {
	c := newCommon("killmatrix", true)
	baseDir := c.fs.String("base-dir", "", "directory of baseline JUnit files, one per seeded bug (required)")
	newDir := c.fs.String("new-dir", "", "directory of new JUnit files, one per seeded bug (required)")
	onlyExpr := c.fs.String("only", "", "regular expression; only check tests whose new name matches")
	c.parse(args)
	if *baseDir == "" || *newDir == "" {
		return false, fmt.Errorf("-base-dir and -new-dir are required")
	}

	m, err := c.mapping()
	if err != nil {
		return false, err
	}
	only, err := compileOnly(*onlyExpr)
	if err != nil {
		return false, err
	}
	findings, err := compareKills(m, *baseDir, *newDir, only)
	if err != nil {
		return false, err
	}
	if len(findings) == 0 {
		return false, fmt.Errorf("no JUnit files found in %s", *baseDir)
	}

	ok := true
	for _, f := range findings {
		switch {
		case f.Missing:
			fmt.Fprintf(w, "MISSING  %s: no result file in the new directory\n", f.Bug)
		case f.Caught == 0:
			fmt.Fprintf(w, "unused   %s: no compared test caught it in the baseline\n", f.Bug)
		case f.ok():
			fmt.Fprintf(w, "ok       %s: all %d tests that caught it still fail\n", f.Bug, f.Caught)
		default:
			fmt.Fprintf(w, "WEAKER   %s: %d of %d tests that caught it no longer do\n", f.Bug, len(f.Lost)+len(f.NotRun), f.Caught)
		}
		printList(w, "  no longer fails", f.Lost)
		printList(w, "  not run", f.NotRun)
		printList(w, "  newly fails", f.Gained)
		ok = ok && f.ok()
	}
	return ok, nil
}

func cmdSkips(w io.Writer, args []string) (bool, error) {
	fs := flag.NewFlagSet("skips", flag.ExitOnError)
	junitFile := fs.String("junit", "", "JUnit file of the run to check (required)")
	skipsFile := fs.String("expected-skips", "test/e2e/expected-skips.txt", "glob patterns of tests that are allowed to skip")
	_ = fs.Parse(args)
	if *junitFile == "" {
		return false, fmt.Errorf("-junit is required")
	}
	results, err := parseJUnitFile(*junitFile)
	if err != nil {
		return false, err
	}
	expected, err := loadPatterns(*skipsFile)
	if err != nil {
		return false, err
	}
	unexpected, unused := checkSkips(results, expected)
	skipped := 0
	for _, r := range results {
		if r.Status == statusSkip {
			skipped++
		}
	}
	printList(w, "UNEXPECTED SKIP ", unexpected)
	printList(w, "did not skip    ", unused)
	fmt.Fprintf(w, "%d tests, %d skipped, %d of them not on the expected-skip list\n", len(results), skipped, len(unexpected))
	return len(unexpected) == 0, nil
}

func cmdConvert(w io.Writer, args []string) (bool, error) {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	dir := fs.String("dir", "test/e2e", "package directory whose test files are rewritten in place")
	suiteType := fs.String("suite", "", "suite type to convert, for example CanarySuite (required)")
	renamesFile := fs.String("renames", "test/e2e/renames.yaml", "YAML map of baseline test name to new test name")
	_ = fs.Parse(args)
	if *suiteType == "" {
		return false, fmt.Errorf("-suite is required")
	}
	entries, err := os.ReadDir(*dir)
	if err != nil {
		return false, err
	}
	var files []sourceFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.ToSlash(filepath.Join(*dir, entry.Name()))
		src, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		files = append(files, sourceFile{Path: path, Src: src})
	}
	renames, err := loadStringMap(*renamesFile)
	if err != nil {
		return false, err
	}
	changed, err := convertSuiteFiles(files, *suiteType, renames)
	if err != nil {
		return false, err
	}
	paths := make([]string, 0, len(changed))
	for path := range changed {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := os.WriteFile(path, changed[path], 0o644); err != nil {
			return false, err
		}
		fmt.Fprintf(w, "converted %s in %s\n", *suiteType, path)
	}
	return true, nil
}
