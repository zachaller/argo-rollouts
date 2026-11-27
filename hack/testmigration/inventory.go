package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// test is one leaf test: either a plain `func TestX(t *testing.T)` or a method
// on a testify suite.
type test struct {
	// Dir is the package directory, relative to the repository root.
	Dir string
	// Name is the function or method name.
	Name string
	// Suite is the suite type the method belongs to. Empty for a plain test.
	Suite string
	// Runner is the top-level test function that runs the suite. Empty for a plain test.
	Runner string
	File   string
	Line   int

	// Receiver is the receiver name of a suite method.
	Receiver string
	// Body is the normalized source text of the whole function body.
	Body string
	// First is the source text of the first statement, and Rest is the
	// normalized text of everything after it.
	First string
	Rest  string
	// CtorVar is set when the first statement is a harness constructor call
	// (`s := newCanaryTest(t)` or `s := fixtures.New(t)`); it holds the variable name.
	CtorVar string
	// CtorOptions is the source text of the arguments the constructor call
	// has after t, for example `fixtures.Serial("why")`. Empty when there
	// are none.
	CtorOptions string
	// Subtests are the static names passed to t.Run / s.Run inside the body.
	Subtests []string
}

// name is how `go test` reports the test: "TestX" or "TestXSuite/TestY".
func (t test) name() string {
	if t.Suite == "" {
		return t.Name
	}
	return t.Runner + "/" + t.Name
}

// id is unique across packages.
func (t test) id() string {
	return t.Dir + ":" + t.name()
}

type inventory struct {
	Tests []test
	// Orphans are Test* methods on types that no test function runs as a suite.
	// They never execute, so they are reported separately.
	Orphans []test
	// EmptyRunners are suite runner functions whose suite has no Test* methods left.
	EmptyRunners []string
}

func (inv inventory) byName(dir string) map[string]test {
	m := map[string]test{}
	for _, t := range inv.Tests {
		if dir == "" || t.Dir == dir {
			m[t.name()] = t
		}
	}
	return m
}

var ctorName = regexp.MustCompile(`^new[A-Z][A-Za-z0-9]*Test$`)

// buildInventory parses the given files and lists every leaf test in them.
func buildInventory(files []sourceFile) (inventory, error) {
	type parsed struct {
		file sourceFile
		ast  *ast.File
	}
	fset := token.NewFileSet()
	byDir := map[string][]parsed{}
	for _, f := range files {
		node, err := parser.ParseFile(fset, f.Path, f.Src, parser.ParseComments)
		if err != nil {
			return inventory{}, fmt.Errorf("parsing %s: %w", f.Path, err)
		}
		dir := path.Dir(f.Path)
		byDir[dir] = append(byDir[dir], parsed{file: f, ast: node})
	}

	var inv inventory
	for dir, pkg := range byDir {
		// First pass: which top-level test functions run which suite types.
		runnerOf := map[string]string{}
		runnerFuncs := map[string]bool{}
		for _, p := range pkg {
			for _, decl := range p.ast.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Body == nil || !isTestFuncName(fn.Name.Name) {
					continue
				}
				for _, suiteType := range suitesRunBy(fn) {
					runnerOf[suiteType] = fn.Name.Name
					runnerFuncs[fn.Name.Name] = true
				}
			}
		}

		// Second pass: collect plain tests and suite methods.
		methodCount := map[string]int{}
		for _, p := range pkg {
			for _, decl := range p.ast.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || !isTestFuncName(fn.Name.Name) {
					continue
				}
				t := test{
					Dir:  dir,
					Name: fn.Name.Name,
					File: p.file.Path,
					Line: fset.Position(fn.Pos()).Line,
				}
				fillBody(&t, fset, p.file.Src, fn)

				if fn.Recv == nil {
					if runnerFuncs[fn.Name.Name] || !hasSingleParam(fn) || fn.Name.Name == "TestMain" {
						continue
					}
					inv.Tests = append(inv.Tests, t)
					continue
				}

				if len(fn.Type.Params.List) != 0 {
					continue
				}
				t.Suite = receiverType(fn)
				t.Receiver = receiverName(fn)
				runner, ok := runnerOf[t.Suite]
				if !ok {
					inv.Orphans = append(inv.Orphans, t)
					continue
				}
				t.Runner = runner
				methodCount[t.Suite]++
				inv.Tests = append(inv.Tests, t)
			}
		}
		for suiteType, runner := range runnerOf {
			if methodCount[suiteType] == 0 {
				inv.EmptyRunners = append(inv.EmptyRunners, dir+":"+runner)
			}
		}
	}

	sort.Slice(inv.Tests, func(i, j int) bool { return inv.Tests[i].id() < inv.Tests[j].id() })
	sort.Slice(inv.Orphans, func(i, j int) bool { return inv.Orphans[i].id() < inv.Orphans[j].id() })
	sort.Strings(inv.EmptyRunners)
	return inv, nil
}

// isTestFuncName applies the `go test` rule: "Test" followed by nothing or by
// a character that is not a lowercase letter.
func isTestFuncName(name string) bool {
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	rest := name[len("Test"):]
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(r)
}

func hasSingleParam(fn *ast.FuncDecl) bool {
	n := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			n++
		} else {
			n += len(field.Names)
		}
	}
	return n == 1
}

func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

// suitesRunBy returns the suite types passed to suite.Run inside fn, for
// example `suite.Run(t, new(CanarySuite))` or `suite.Run(t, &CanarySuite{})`.
func suitesRunBy(fn *ast.FuncDecl) []string {
	var types []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Run" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "suite" {
			return true
		}
		switch arg := call.Args[1].(type) {
		case *ast.CallExpr: // new(T)
			if fun, ok := arg.Fun.(*ast.Ident); ok && fun.Name == "new" && len(arg.Args) == 1 {
				if ident, ok := arg.Args[0].(*ast.Ident); ok {
					types = append(types, ident.Name)
				}
			}
		case *ast.UnaryExpr: // &T{}
			if lit, ok := arg.X.(*ast.CompositeLit); ok {
				if ident, ok := lit.Type.(*ast.Ident); ok {
					types = append(types, ident.Name)
				}
			}
		}
		return true
	})
	return types
}

func fillBody(t *test, fset *token.FileSet, src []byte, fn *ast.FuncDecl) {
	lbrace := fset.Position(fn.Body.Lbrace).Offset
	rbrace := fset.Position(fn.Body.Rbrace).Offset
	t.Body = normalizeBody(string(src[lbrace+1 : rbrace]))
	t.Rest = t.Body

	if len(fn.Body.List) > 0 {
		first := fn.Body.List[0]
		start := fset.Position(first.Pos()).Offset
		end := fset.Position(first.End()).Offset
		t.First = string(src[start:end])
		t.Rest = normalizeBody(string(src[end:rbrace]))
		t.CtorVar = constructorVar(first)
		if t.CtorVar != "" {
			call := first.(*ast.AssignStmt).Rhs[0].(*ast.CallExpr)
			if len(call.Args) > 1 {
				from := fset.Position(call.Args[1].Pos()).Offset
				to := fset.Position(call.Args[len(call.Args)-1].End()).Offset
				t.CtorOptions = string(src[from:to])
			}
		}
	}

	seen := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Run" {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name := strings.Trim(lit.Value, "\"`")
		if !seen[name] {
			seen[name] = true
			t.Subtests = append(t.Subtests, name)
		}
		return true
	})
}

// constructorVar reports the variable assigned by a harness constructor
// statement, or "" when stmt is not one. Accepted forms:
//
//	s := newCanaryTest(t)
//	s := fixtures.New(t, ...)
func constructorVar(stmt ast.Stmt) string {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return ""
	}
	lhs, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return ""
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return ""
	}
	if arg, ok := call.Args[0].(*ast.Ident); !ok || arg.Name != "t" {
		return ""
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if ctorName.MatchString(fun.Name) {
			return lhs.Name
		}
	case *ast.SelectorExpr:
		if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "fixtures" && fun.Sel.Name == "New" {
			return lhs.Name
		}
	}
	return ""
}

// normalizeBody trims trailing whitespace on each line and drops leading and
// trailing blank lines, so that only meaningful differences remain.
func normalizeBody(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
