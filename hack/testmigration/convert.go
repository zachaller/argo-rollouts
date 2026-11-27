package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// suiteUse matches a use of the testify suite package, such as suite.Run.
var suiteUse = regexp.MustCompile(`\bsuite\.[A-Z]`)

// convertSuite rewrites one testify suite in src into plain test functions.
//
//	type CanarySuite struct{ fixtures.E2ESuite }
//	func TestCanarySuite(t *testing.T) { suite.Run(t, new(CanarySuite)) }
//	func (s *CanarySuite) SetupSuite() { s.E2ESuite.SetupSuite(); <setup> }
//	func (s *CanarySuite) TestX() { <body> }
//
// becomes
//
//	func newCanaryTest(t *testing.T) *fixtures.Test { s := fixtures.New(t); <setup>; return s }
//	func TestX(t *testing.T) { s := newCanaryTest(t); <body> }
//
// Only the declarations are edited, as text; every test body is left
// byte-for-byte as it was. renames maps "TestCanarySuite/TestX" to a new
// function name.
//
// A suite with helper methods gets a small type of its own, so that calls such
// as s.check(...) in test bodies keep working unchanged:
//
//	type apisixTest struct{ *fixtures.Test }
//	func newAPISIXTest(t *testing.T) *apisixTest { s := &apisixTest{fixtures.New(t)}; ...; return s }
//	func (s *apisixTest) check(...) { <body> }
//
// Helper functions that take the suite as a parameter take the new type instead.
//
// The rewrite refuses suites it cannot convert mechanically: ones with suite
// hooks other than SetupSuite.
func convertSuite(path string, src []byte, suiteType string, renames map[string]string) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	offset := func(p token.Pos) int { return fset.Position(p).Offset }

	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	// declRange covers a declaration with its doc comment and the blank line after it.
	declRange := func(start, end token.Pos) (int, int) {
		e := offset(end)
		for e < len(src) && src[e] == '\n' {
			e++
		}
		return offset(start), e
	}

	baseName := strings.TrimSuffix(suiteType, "Suite")
	ctor := "new" + baseName + "Test"
	var (
		typeDecl   *ast.GenDecl
		runner     *ast.FuncDecl
		setup      *ast.FuncDecl
		recvName   = "s"
		converted  int
		runnerName string
		helpers    []*ast.FuncDecl
		testFuncs  []*ast.FuncDecl
	)

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE || len(d.Specs) != 1 {
				continue
			}
			if spec := d.Specs[0].(*ast.TypeSpec); spec.Name.Name == suiteType {
				typeDecl = d
			}
		case *ast.FuncDecl:
			if d.Recv == nil && d.Body != nil {
				for _, t := range suitesRunBy(d) {
					if t == suiteType {
						runner = d
						runnerName = d.Name.Name
					}
				}
			}
		}
	}
	if typeDecl == nil {
		return nil, fmt.Errorf("%s: suite type %s not found", path, suiteType)
	}
	if runner == nil {
		return nil, fmt.Errorf("%s: no test function runs %s with suite.Run", path, suiteType)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || receiverType(fn) != suiteType {
			continue
		}
		switch {
		case fn.Name.Name == "SetupSuite":
			setup = fn
			if name := receiverName(fn); name != "" {
				recvName = name
			}
		case isTestFuncName(fn.Name.Name) && len(fn.Type.Params.List) == 0:
			recv := receiverName(fn)
			if recv == "" {
				return nil, fmt.Errorf("%s: %s.%s has no receiver name", path, suiteType, fn.Name.Name)
			}
			name := fn.Name.Name
			if r, ok := renames[runnerName+"/"+name]; ok {
				name = r
			}
			// fn.Pos() is the "func" keyword, so a doc comment is kept as it is.
			edits = append(edits, edit{
				start: offset(fn.Pos()),
				end:   offset(fn.Body.Lbrace) + 1,
				text:  fmt.Sprintf("func %s(t *testing.T) {\n\t%s := %s(t)", name, recv, ctor),
			})
			testFuncs = append(testFuncs, fn)
			converted++
		case suiteHooks[fn.Name.Name]:
			return nil, fmt.Errorf("%s: %s has the suite hook %s, which cannot be converted mechanically", path, suiteType, fn.Name.Name)
		default:
			helpers = append(helpers, fn)
		}
	}
	if converted == 0 {
		return nil, fmt.Errorf("%s: %s has no test methods", path, suiteType)
	}

	// The constructor takes the place of the suite type declaration.
	var setupBody string
	if setup != nil {
		stmts := setup.Body.List
		if len(stmts) == 0 || !isEmbeddedSetupCall(stmts[0], recvName) {
			return nil, fmt.Errorf("%s: %s.SetupSuite does not start with %s.E2ESuite.SetupSuite()", path, suiteType, recvName)
		}
		setupBody = strings.TrimRight(string(src[offset(stmts[0].End()):offset(setup.Body.Rbrace)]), " \t\n")
	}
	// The type that replaces the suite type wherever it is still named: the
	// receivers of helper methods and the parameters of helper functions.
	newType := "fixtures.Test"
	newValue := "fixtures.New(t)"
	var sb strings.Builder
	if len(helpers) > 0 {
		newType = lowerCamel(baseName) + "Test"
		newValue = fmt.Sprintf("&%s{fixtures.New(t)}", newType)
		fmt.Fprintf(&sb, "// %s is the environment of one test, with the helpers %s had.\n", newType, suiteType)
		fmt.Fprintf(&sb, "type %s struct {\n\t*fixtures.Test\n}\n\n", newType)
	}
	if setup != nil {
		fmt.Fprintf(&sb, "// %s sets up one test. It does what %s.SetupSuite did once for the whole suite.\n", ctor, suiteType)
	} else {
		fmt.Fprintf(&sb, "// %s sets up one test.\n", ctor)
	}
	fmt.Fprintf(&sb, "func %s(t *testing.T) *%s {\n\t%s := %s", ctor, newType, recvName, newValue)
	sb.WriteString(setupBody)
	fmt.Fprintf(&sb, "\n\treturn %s\n}\n\n", recvName)

	// Retarget every remaining mention of *SuiteType. Declarations that are
	// replaced or removed as a whole are skipped.
	replaced := map[ast.Node]bool{typeDecl: true, runner: true}
	if setup != nil {
		replaced[setup] = true
	}
	for _, decl := range file.Decls {
		if replaced[decl] {
			continue
		}
		fn, isFunc := decl.(*ast.FuncDecl)
		isTest := false
		for _, tf := range testFuncs {
			if isFunc && tf == fn {
				isTest = true
			}
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			// The signature of a converted test method is rewritten as a whole.
			if isTest && n == ast.Node(fn.Recv) {
				return false
			}
			star, ok := n.(*ast.StarExpr)
			if !ok {
				return true
			}
			if ident, ok := star.X.(*ast.Ident); ok && ident.Name == suiteType {
				edits = append(edits, edit{start: offset(star.Pos()), end: offset(star.End()), text: "*" + newType})
				return false
			}
			return true
		})
	}

	typeStart := typeDecl.Pos()
	if typeDecl.Doc != nil {
		typeStart = typeDecl.Doc.Pos()
	}
	s, e := declRange(typeStart, typeDecl.End())
	edits = append(edits, edit{start: s, end: e, text: sb.String()})

	for _, fn := range []*ast.FuncDecl{runner, setup} {
		if fn == nil {
			continue
		}
		start := fn.Pos()
		if fn.Doc != nil {
			start = fn.Doc.Pos()
		}
		s, e := declRange(start, fn.End())
		edits = append(edits, edit{start: s, end: e})
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), src...)
	for _, ed := range edits {
		out = append(out[:ed.start], append([]byte(ed.text), out[ed.end:]...)...)
	}

	// Drop the testify suite import once nothing refers to it.
	if !suiteUse.MatchString(stripImports(string(out))) {
		out = []byte(strings.Replace(string(out), "\t\"github.com/stretchr/testify/suite\"\n", "", 1))
	}

	formatted, err := format.Source(out)
	if err != nil {
		return nil, fmt.Errorf("%s: converted source does not parse: %w", path, err)
	}
	return formatted, nil
}

// isEmbeddedSetupCall reports whether stmt is `<recv>.E2ESuite.SetupSuite()`.
func isEmbeddedSetupCall(stmt ast.Stmt, recv string) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "SetupSuite" {
		return false
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "E2ESuite" {
		return false
	}
	ident, ok := inner.X.(*ast.Ident)
	return ok && ident.Name == recv
}

// stripImports removes the import block so that an import path is not mistaken
// for a use of the package.
func stripImports(src string) string {
	start := strings.Index(src, "import (")
	if start < 0 {
		return src
	}
	end := strings.Index(src[start:], ")")
	if end < 0 {
		return src
	}
	return src[:start] + src[start+end+1:]
}

// suiteHooks are the testify suite methods that run around tests. A suite that
// defines one (other than SetupSuite) needs a person to decide what becomes of it.
var suiteHooks = map[string]bool{
	"TearDownSuite":   true,
	"SetupTest":       true,
	"TearDownTest":    true,
	"BeforeTest":      true,
	"AfterTest":       true,
	"SetupSubTest":    true,
	"TearDownSubTest": true,
}

// lowerCamel lower-cases the leading capitals of a name so that it reads as an
// unexported identifier: "BlueGreen" becomes "blueGreen", "APISIX" becomes
// "apisix", "AWSLoadBalancer" becomes "awsLoadBalancer".
func lowerCamel(name string) string {
	runes := []rune(name)
	for i := range runes {
		if !unicode.IsUpper(runes[i]) {
			break
		}
		// Keep the capital that starts the next word: the "L" in "AWSLoad".
		if i > 0 && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
			break
		}
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}
