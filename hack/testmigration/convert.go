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
// The rewrite refuses suites it cannot convert mechanically: ones with helper
// methods, or with suite hooks other than SetupSuite.
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

	ctor := "new" + strings.TrimSuffix(suiteType, "Suite") + "Test"
	var (
		typeDecl   *ast.GenDecl
		runner     *ast.FuncDecl
		setup      *ast.FuncDecl
		recvName   = "s"
		converted  int
		runnerName string
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
			converted++
		default:
			return nil, fmt.Errorf("%s: %s has method %s, which cannot be converted mechanically", path, suiteType, fn.Name.Name)
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
	var sb strings.Builder
	fmt.Fprintf(&sb, "// %s sets up one test. It does what %s.SetupSuite did once for the whole suite.\n", ctor, suiteType)
	fmt.Fprintf(&sb, "func %s(t *testing.T) *fixtures.Test {\n\t%s := fixtures.New(t)", ctor, recvName)
	sb.WriteString(setupBody)
	fmt.Fprintf(&sb, "\n\treturn %s\n}\n\n", recvName)

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
