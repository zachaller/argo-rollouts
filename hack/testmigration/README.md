# testmigration

Checks that the e2e test migration (testify suites to plain parallel test
functions) does not lose a test or silently change one. Every command compares
a baseline git ref with the working tree and exits non-zero when a check fails.

Run from the repository root:

```
go run ./hack/testmigration <command> -base <baseline-ref> [flags]
```

| Command | Fails when |
|---|---|
| `inventory` | never; lists every test at a ref (`-ref`), with `-subtests` for static subtest names |
| `inventory-diff` | a baseline test has no counterpart, has two, or two baseline tests map to the same test |
| `bodydiff` | a test body differs from the baseline and is not listed in the exceptions file |
| `junitdiff` | a test fails, is newly skipped, skips without being on the expected-skip list, or has no result |
| `killmatrix` | a test that failed under a seeded bug in the baseline passes under the same bug now, without a reason recorded in `seeded-bug-exceptions.yaml` next to the baseline directory (or the file given with `-exceptions`) |
| `skips` | a run skipped a test that is not on the expected-skip list (needs only the run's JUnit file; this is the check CI runs) |
| `convert` | the suite cannot be converted mechanically (it has a suite hook other than `SetupSuite`, or its `SetupSuite` does not start with the embedded call) |

`convert -suite <SuiteType>` rewrites one testify suite in `test/e2e` (or
`-dir`) into plain test functions, in place, in every file that has a part of
it:

- each test method becomes a plain function that starts with the constructor
  call;
- the suite type becomes the constructor, with the rest of `SetupSuite` as its
  body;
- helper methods keep their receiver name and move to a small wrapper type
  around `*fixtures.Test`; helper functions that took the suite take that type.

It edits only those declarations, as text. It does not run gofmt over the file,
so every test body stays byte-for-byte as it was, also in a file that was not
gofmt-clean to begin with.

## How tests are matched

A baseline test keeps its name, or a suite method `TestXSuite/TestY` becomes the
plain function `TestY`. Any other new name must be listed in
`test/e2e/renames.yaml` (baseline name to new name).

A converted test must start with a harness constructor that assigns the same
variable name the suite method used as its receiver, for example
`s := newCanaryTest(t)`. `bodydiff` ignores that one line and requires the rest
of the body to be byte-identical to the old method body.

## Files

- `test/e2e/renames.yaml`: renames that the default rule does not cover.
- `test/e2e/body-exceptions.yaml`: test name to the reason its body may differ.
  An entry that no longer differs is reported as stale.
- `test/e2e/expected-skips.txt`: glob patterns of tests allowed to skip.
- `hack/testmigration/baseline/`: results of the unconverted serial suite.

The repository's `.gitignore` ignores `junit*`. Do not give a file that has to
be committed a name starting with `junit`; git skips it without a word.

## Examples

```
go run ./hack/testmigration inventory-diff -base upstream/master
go run ./hack/testmigration bodydiff -base upstream/master
go run ./hack/testmigration junitdiff -base upstream/master \
    -base-junit hack/testmigration/baseline/e2e-results-k8s1.37.xml \
    -new-junit junit-e2e-test.xml -only '^TestCanary'
go run ./hack/testmigration killmatrix -base upstream/master \
    -base-dir baseline-bugs -new-dir new-bugs -only '^TestCanary'
```
