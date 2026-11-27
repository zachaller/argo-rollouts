package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const baseSrc = `package e2e

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type CanarySuite struct{ suite.Suite }

func TestCanarySuite(t *testing.T) {
	suite.Run(t, new(CanarySuite))
}

func (s *CanarySuite) SetupSuite() {}

func (s *CanarySuite) TestScale() {
	s.Given().
		HealthyRollout("@a.yaml").
		When().
		UpdateSpec()
}

func (s *CanarySuite) TestEphemeralMetadata() {
	s.T().Log("canary")
}

func (s *CanarySuite) helper() {}

type BlueGreenSuite struct{ suite.Suite }

func TestBlueGreenSuite(t *testing.T) {
	suite.Run(t, &BlueGreenSuite{})
}

func (s *BlueGreenSuite) TestEphemeralMetadata() {
	s.T().Log("bluegreen")
}

type NeverRunSuite struct{}

func (s *NeverRunSuite) TestOrphan() {}

func TestPlain(t *testing.T) {
	t.Run("sub one", func(t *testing.T) {})
}

func TestMain(m *testing.M) {}

func Testify() {}
`

func inv(t *testing.T, src string) inventory {
	t.Helper()
	out, err := buildInventory([]sourceFile{{Path: "test/e2e/x_test.go", Src: []byte(src)}})
	require.NoError(t, err)
	return out
}

func names(tests []test) []string {
	var out []string
	for _, t := range tests {
		out = append(out, t.name())
	}
	return out
}

func TestIsTestFuncName(t *testing.T) {
	assert.True(t, isTestFuncName("Test"))
	assert.True(t, isTestFuncName("TestFoo"))
	assert.True(t, isTestFuncName("Test_foo"))
	assert.True(t, isTestFuncName("Test1"))
	assert.False(t, isTestFuncName("Testify"))
	assert.False(t, isTestFuncName("SetupSuite"))
}

func TestBuildInventory(t *testing.T) {
	got := inv(t, baseSrc)

	assert.Equal(t, []string{
		"TestBlueGreenSuite/TestEphemeralMetadata",
		"TestCanarySuite/TestEphemeralMetadata",
		"TestCanarySuite/TestScale",
		"TestPlain",
	}, names(got.Tests))

	require.Len(t, got.Orphans, 1)
	assert.Equal(t, "NeverRunSuite", got.Orphans[0].Suite)
	assert.Equal(t, "TestOrphan", got.Orphans[0].Name)
	assert.Empty(t, got.EmptyRunners)

	scale := got.byName("test/e2e")["TestCanarySuite/TestScale"]
	assert.Equal(t, "s", scale.Receiver)
	assert.Equal(t, "CanarySuite", scale.Suite)
	assert.Equal(t, "TestCanarySuite", scale.Runner)
	assert.Equal(t, "test/e2e:TestCanarySuite/TestScale", scale.id())
	assert.Equal(t, "\ts.Given().\n\t\tHealthyRollout(\"@a.yaml\").\n\t\tWhen().\n\t\tUpdateSpec()", scale.Body)

	assert.Equal(t, []string{"sub one"}, got.byName("")["TestPlain"].Subtests)
}

func TestBuildInventoryEmptyRunner(t *testing.T) {
	got := inv(t, `package e2e

func TestCanarySuite(t *testing.T) { suite.Run(t, new(CanarySuite)) }
`)
	assert.Empty(t, got.Tests)
	assert.Equal(t, []string{"test/e2e:TestCanarySuite"}, got.EmptyRunners)
}

func TestConstructorDetection(t *testing.T) {
	got := inv(t, `package e2e

func TestA(t *testing.T) {
	s := newCanaryTest(t)
	s.Given()
}

func TestB(t *testing.T) {
	s := fixtures.New(t, fixtures.Serial("needs the whole node"))
	s.Given()
}

func TestC(t *testing.T) {
	s := somethingElse(t)
	s.Given()
}

func TestD(t *testing.T) {
	s := newCanaryTest(other)
	s.Given()
}
`)
	byName := got.byName("")
	assert.Equal(t, "s", byName["TestA"].CtorVar)
	assert.Equal(t, "\ts.Given()", byName["TestA"].Rest)
	assert.Equal(t, "s", byName["TestB"].CtorVar)
	assert.Empty(t, byName["TestC"].CtorVar)
	assert.Empty(t, byName["TestD"].CtorVar)
}

const convertedSrc = `package e2e

import "testing"

func newCanaryTest(t *testing.T) *fixtures.Test { return fixtures.New(t) }

func TestScale(t *testing.T) {
	s := newCanaryTest(t)
	s.Given().
		HealthyRollout("@a.yaml").
		When().
		UpdateSpec()
}

func TestCanaryEphemeralMetadata(t *testing.T) {
	s := newCanaryTest(t)
	s.T().Log("canary")
}

type BlueGreenSuite struct{ suite.Suite }

func TestBlueGreenSuite(t *testing.T) {
	suite.Run(t, &BlueGreenSuite{})
}

func (s *BlueGreenSuite) TestEphemeralMetadata() {
	s.T().Log("bluegreen")
}

func TestPlain(t *testing.T) {
	t.Run("sub one", func(t *testing.T) {})
}
`

var canaryRename = map[string]string{"TestCanarySuite/TestEphemeralMetadata": "TestCanaryEphemeralMetadata"}

func TestMapTestsConversion(t *testing.T) {
	m := mapTests(inv(t, baseSrc), inv(t, convertedSrc), canaryRename)

	assert.True(t, m.ok())
	assert.Equal(t, map[string]int{kindUnchanged: 2, kindConverted: 1, kindRenamed: 1}, m.counts())
	assert.Empty(t, m.Added)

	findings, stale := compareBodies(m, nil)
	assert.Empty(t, findings)
	assert.Empty(t, stale)
}

func TestMapTestsProblems(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		head := inv(t, `package e2e
func TestPlain(t *testing.T) {}
`)
		m := mapTests(inv(t, baseSrc), head, nil)
		assert.False(t, m.ok())
		assert.Equal(t, []string{
			"TestBlueGreenSuite/TestEphemeralMetadata",
			"TestCanarySuite/TestEphemeralMetadata",
			"TestCanarySuite/TestScale",
		}, names(m.Missing))
	})

	t.Run("collision without a rename", func(t *testing.T) {
		head := inv(t, `package e2e
func TestScale(t *testing.T) {}
func TestEphemeralMetadata(t *testing.T) {}
func TestPlain(t *testing.T) {}
`)
		m := mapTests(inv(t, baseSrc), head, nil)
		assert.False(t, m.ok())
		require.Len(t, m.Collisions, 1)
		assert.Len(t, m.Collisions["test/e2e:TestEphemeralMetadata"], 2)
	})

	t.Run("old and new form both present", func(t *testing.T) {
		head := inv(t, baseSrc+`
func TestScale(t *testing.T) {}
`)
		m := mapTests(inv(t, baseSrc), head, nil)
		assert.False(t, m.ok())
		assert.Equal(t, []string{"TestCanarySuite/TestScale"}, names(m.Duplicated))
	})

	t.Run("added and stale rename", func(t *testing.T) {
		head := inv(t, baseSrc+`
func TestBrandNew(t *testing.T) {}
`)
		m := mapTests(inv(t, baseSrc), head, map[string]string{"TestGoneSuite/TestGone": "TestGone"})
		assert.False(t, m.ok())
		assert.Equal(t, []string{"TestBrandNew"}, names(m.Added))
		assert.Equal(t, []string{"TestGoneSuite/TestGone"}, m.StaleRenames)
	})
}

func TestCompareBodies(t *testing.T) {
	base := inv(t, baseSrc)

	t.Run("changed body is reported with a diff", func(t *testing.T) {
		head := inv(t, `package e2e
func TestScale(t *testing.T) {
	s := newCanaryTest(t)
	s.Given().
		HealthyRollout("@b.yaml").
		When().
		UpdateSpec()
}
`)
		m := mapTests(base, head, nil)
		findings, _ := compareBodies(m, nil)
		require.Len(t, findings, 1)
		assert.False(t, findings[0].accepted())
		assert.Contains(t, findings[0].Diff, "- \t\tHealthyRollout(\"@a.yaml\").")
		assert.Contains(t, findings[0].Diff, "+ \t\tHealthyRollout(\"@b.yaml\").")
	})

	t.Run("missing constructor", func(t *testing.T) {
		head := inv(t, `package e2e
func TestScale(t *testing.T) {
	s.Given().
		HealthyRollout("@a.yaml").
		When().
		UpdateSpec()
}
`)
		findings, _ := compareBodies(mapTests(base, head, nil), nil)
		require.Len(t, findings, 1)
		assert.Contains(t, findings[0].Problem, "not a harness constructor")
	})

	t.Run("constructor variable differs from the receiver", func(t *testing.T) {
		head := inv(t, `package e2e
func TestScale(t *testing.T) {
	e := newCanaryTest(t)
	e.Given()
}
`)
		findings, _ := compareBodies(mapTests(base, head, nil), nil)
		require.Len(t, findings, 1)
		assert.Contains(t, findings[0].Problem, `assigns "e"`)
	})

	t.Run("exceptions are accepted and stale ones reported", func(t *testing.T) {
		head := inv(t, `package e2e
func TestScale(t *testing.T) {
	s := newCanaryTest(t)
	s.Given()
}
`)
		findings, stale := compareBodies(mapTests(base, head, nil), map[string]string{
			"TestScale": "sleep replaced by a condition wait",
			"TestOther": "no longer differs",
		})
		require.Len(t, findings, 1)
		assert.True(t, findings[0].accepted())
		assert.Equal(t, []string{"TestOther"}, stale)
	})

	t.Run("unconverted test that changed is reported", func(t *testing.T) {
		head := inv(t, `package e2e
func TestBlueGreenSuite(t *testing.T) { suite.Run(t, &BlueGreenSuite{}) }
func (s *BlueGreenSuite) TestEphemeralMetadata() {
	s.T().Log("changed")
}
`)
		findings, _ := compareBodies(mapTests(base, head, nil), nil)
		require.Len(t, findings, 1)
		assert.Equal(t, "TestBlueGreenSuite/TestEphemeralMetadata", findings[0].Pair.Head.name())
	})
}

func TestLineDiff(t *testing.T) {
	assert.Empty(t, lineDiff("a\nb", "a\nb", 1))
	assert.Equal(t, "  a\n- b\n+ x\n  c\n", lineDiff("a\nb\nc", "a\nx\nc", 1))
	assert.Equal(t, "+ x\n  ...\n- z\n", lineDiff("a\nb\nc\nz", "x\na\nb\nc", 0))
}

const baseJUnit = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
	<testsuite name="github.com/argoproj/argo-rollouts/test/e2e">
		<testcase classname="e2e" name="TestCanarySuite"></testcase>
		<testcase classname="e2e" name="TestCanarySuite/TestScale"></testcase>
		<testcase classname="e2e" name="TestCanarySuite/TestEphemeralMetadata"><failure message="Failed"></failure></testcase>
		<testcase classname="e2e" name="TestCanarySuite/TestEphemeralMetadata"></testcase>
		<testcase classname="e2e" name="TestBlueGreenSuite"><skipped message="Skipped"></skipped></testcase>
		<testcase classname="e2e" name="TestPlain"></testcase>
	</testsuite>
</testsuites>`

func TestParseJUnit(t *testing.T) {
	results, err := parseJUnit([]byte(baseJUnit))
	require.NoError(t, err)

	assert.Equal(t, result{Status: statusPass, Attempts: 1}, results["TestCanarySuite/TestScale"])
	flaky := results["TestCanarySuite/TestEphemeralMetadata"]
	assert.Equal(t, result{Status: statusPass, Attempts: 2, Failures: 1}, flaky)
	assert.True(t, flaky.flaky())
	assert.Equal(t, statusSkip, results["TestBlueGreenSuite"].Status)

	bare, err := parseJUnit([]byte(`<testsuite><testcase name="TestA"><error/></testcase></testsuite>`))
	require.NoError(t, err)
	assert.Equal(t, statusFail, bare["TestA"].Status)
}

func TestStatusOfInheritsSuiteSkip(t *testing.T) {
	results, err := parseJUnit([]byte(baseJUnit))
	require.NoError(t, err)
	byName := inv(t, baseSrc).byName("")

	assert.Equal(t, statusSkip, statusOf(results, byName["TestBlueGreenSuite/TestEphemeralMetadata"]).Status)
	assert.Equal(t, statusPass, statusOf(results, byName["TestCanarySuite/TestScale"]).Status)
	assert.Equal(t, statusNotRun, statusOf(map[string]result{}, byName["TestPlain"]).Status)
}

func TestCompareJUnit(t *testing.T) {
	m := mapTests(inv(t, baseSrc), inv(t, convertedSrc), canaryRename)
	baseResults, err := parseJUnit([]byte(baseJUnit))
	require.NoError(t, err)
	skips := []string{"TestBlueGreenSuite/*"}

	t.Run("same outcome", func(t *testing.T) {
		newResults := map[string]result{
			"TestScale":                   {Status: statusPass},
			"TestCanaryEphemeralMetadata": {Status: statusPass},
			"TestBlueGreenSuite":          {Status: statusSkip},
			"TestPlain":                   {Status: statusPass},
		}
		rep := compareJUnit(m, baseResults, newResults, skips, nil)
		assert.True(t, rep.ok())
		assert.Equal(t, 4, rep.Compared)
		assert.Empty(t, rep.StaleExpectedSkips)
	})

	t.Run("every kind of failure", func(t *testing.T) {
		newResults := map[string]result{
			"TestScale":                                {Status: statusFail},
			"TestCanaryEphemeralMetadata":              {Status: statusSkip},
			"TestBlueGreenSuite/TestEphemeralMetadata": {Status: statusPass, Attempts: 2, Failures: 1},
		}
		rep := compareJUnit(m, baseResults, newResults, skips, nil)
		assert.False(t, rep.ok())
		assert.Equal(t, []string{"TestScale"}, rep.Regressions)
		assert.Equal(t, []string{"TestCanaryEphemeralMetadata"}, rep.NewlySkipped)
		assert.Equal(t, []string{"TestCanaryEphemeralMetadata"}, rep.UnexpectedSkips)
		assert.Equal(t, []string{"TestPlain"}, rep.NotRun)
		assert.Equal(t, []string{"TestBlueGreenSuite/TestEphemeralMetadata"}, rep.NowRuns)
		assert.Equal(t, []string{"TestBlueGreenSuite/TestEphemeralMetadata (1 failed attempts)"}, rep.Flaky)
		assert.Equal(t, skips, rep.StaleExpectedSkips)
	})

	t.Run("only restricts the check", func(t *testing.T) {
		newResults := map[string]result{"TestScale": {Status: statusPass}}
		rep := compareJUnit(m, baseResults, newResults, skips, regexp.MustCompile(`^TestScale$`))
		assert.True(t, rep.ok())
		assert.Equal(t, 1, rep.Compared)
	})
}

func TestLoadPatterns(t *testing.T) {
	file := filepath.Join(t.TempDir(), "skips.txt")
	require.NoError(t, os.WriteFile(file, []byte("# comment\n\nTestA  # needs AWS\nTestBSuite/*\n"), 0o644))
	patterns, err := loadPatterns(file)
	require.NoError(t, err)
	assert.Equal(t, []string{"TestA", "TestBSuite/*"}, patterns)

	_, ok := matchAny(patterns, "TestBSuite/TestX")
	assert.True(t, ok)
	_, ok = matchAny(patterns, "TestC")
	assert.False(t, ok)

	missing, err := loadPatterns(filepath.Join(t.TempDir(), "absent.txt"))
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestCompareKills(t *testing.T) {
	m := mapTests(inv(t, baseSrc), inv(t, convertedSrc), canaryRename)
	baseDir, newDir := t.TempDir(), t.TempDir()
	write := func(dir, bug, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, bug+".xml"), []byte("<testsuites><testsuite>"+body+"</testsuite></testsuites>"), 0o644))
	}
	failed := func(name string) string { return `<testcase name="` + name + `"><failure/></testcase>` }
	passed := func(name string) string { return `<testcase name="` + name + `"></testcase>` }

	// Bug 01 is still caught by the same test.
	write(baseDir, "01-still-caught", failed("TestCanarySuite/TestScale")+passed("TestPlain"))
	write(newDir, "01-still-caught", failed("TestScale")+passed("TestPlain"))
	// Bug 02 was caught before and passes now.
	write(baseDir, "02-lost", failed("TestCanarySuite/TestScale")+passed("TestPlain"))
	write(newDir, "02-lost", passed("TestScale")+failed("TestPlain"))
	// Bug 03 has no new result file.
	write(baseDir, "03-missing", failed("TestPlain"))

	findings, err := compareKills(m, baseDir, newDir, nil)
	require.NoError(t, err)
	require.Len(t, findings, 3)

	assert.True(t, findings[0].ok())
	assert.Equal(t, 1, findings[0].Caught)

	assert.False(t, findings[1].ok())
	assert.Equal(t, []string{"TestScale"}, findings[1].Lost)
	assert.Equal(t, []string{"TestPlain"}, findings[1].Gained)

	assert.True(t, findings[2].Missing)
	assert.False(t, findings[2].ok())
}

const suiteSrc = `package e2e

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

// CanarySuite tests canaries.
type CanarySuite struct {
	fixtures.E2ESuite
}

func TestCanarySuite(t *testing.T) {
	suite.Run(t, new(CanarySuite))
}

func (s *CanarySuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	s.ApplyManifests("@functional/analysistemplate-sleep-job.yaml")
}

// TestScale checks scaling.
func (s *CanarySuite) TestScale() {
	s.Given().
		HealthyRollout("@a.yaml").
		Then().
		Assert(func(t *fixtures.Then) {
			s.T().Log("checked")
		})
}

func (s *CanarySuite) TestEphemeralMetadata() {
	s.T().Log("canary")
}
`

func TestConvertSuite(t *testing.T) {
	renames := map[string]string{"TestCanarySuite/TestEphemeralMetadata": "TestCanaryEphemeralMetadata"}
	out, err := convertSuite("test/e2e/x_test.go", []byte(suiteSrc), "CanarySuite", renames)
	require.NoError(t, err)

	assert.Equal(t, `package e2e

import (
	"testing"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

// newCanaryTest sets up one test. It does what CanarySuite.SetupSuite did once for the whole suite.
func newCanaryTest(t *testing.T) *fixtures.Test {
	s := fixtures.New(t)
	s.ApplyManifests("@functional/analysistemplate-sleep-job.yaml")
	return s
}

// TestScale checks scaling.
func TestScale(t *testing.T) {
	s := newCanaryTest(t)
	s.Given().
		HealthyRollout("@a.yaml").
		Then().
		Assert(func(t *fixtures.Then) {
			s.T().Log("checked")
		})
}

func TestCanaryEphemeralMetadata(t *testing.T) {
	s := newCanaryTest(t)
	s.T().Log("canary")
}
`, string(out))

	// The checks the migration relies on accept the generated code as is.
	base := inv(t, suiteSrc)
	head, err := buildInventory([]sourceFile{{Path: "test/e2e/x_test.go", Src: out}})
	require.NoError(t, err)
	m := mapTests(base, head, renames)
	assert.True(t, m.ok())
	findings, _ := compareBodies(m, nil)
	assert.Empty(t, findings)
}

func TestConvertSuiteRefusals(t *testing.T) {
	t.Run("helper method", func(t *testing.T) {
		_, err := convertSuite("x_test.go", []byte(suiteSrc+"\nfunc (s *CanarySuite) helper() {}\n"), "CanarySuite", nil)
		assert.ErrorContains(t, err, "has method helper")
	})
	t.Run("unknown suite", func(t *testing.T) {
		_, err := convertSuite("x_test.go", []byte(suiteSrc), "OtherSuite", nil)
		assert.ErrorContains(t, err, "not found")
	})
	t.Run("setup that does not call the embedded setup first", func(t *testing.T) {
		src := strings.Replace(suiteSrc, "\ts.E2ESuite.SetupSuite()\n", "", 1)
		_, err := convertSuite("x_test.go", []byte(src), "CanarySuite", nil)
		assert.ErrorContains(t, err, "does not start with")
	})
}
