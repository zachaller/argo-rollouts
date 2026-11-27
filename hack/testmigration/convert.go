package main

import (
	"bytes"
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

// convertSuite rewrites one testify suite into plain test functions. It is
// convertSuiteFiles for a suite that lives in a single file.
func convertSuite(path string, src []byte, suiteType string, renames map[string]string) ([]byte, error) {
	out, err := convertSuiteFiles([]sourceFile{{Path: path, Src: src}}, suiteType, renames)
	if err != nil {
		return nil, err
	}
	return out[path], nil
}

// convertSuiteFiles rewrites one testify suite into plain test functions.
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
// files are the test files of one package. The suite's methods may be spread
// over several of them. The result holds the new content of every file that
// changed, by path.
//
// The rewrite refuses suites it cannot convert mechanically: ones with suite
// hooks other than SetupSuite.
func convertSuiteFiles(files []sourceFile, suiteType string, renames map[string]string) (map[string][]byte, error) {
	type parsed struct {
		file sourceFile
		ast  *ast.File
	}
	fset := token.NewFileSet()
	var pkg []parsed
	srcByPath := map[string][]byte{}
	for _, f := range files {
		node, err := parser.ParseFile(fset, f.Path, f.Src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		pkg = append(pkg, parsed{file: f, ast: node})
		srcByPath[f.Path] = f.Src
	}
	offset := func(p token.Pos) int { return fset.Position(p).Offset }

	// First pass over the whole package: where the suite is declared and run,
	// its SetupSuite, and whether it has helper methods.
	var (
		typeDecl   *ast.GenDecl
		typeFile   string
		runner     *ast.FuncDecl
		runnerName string
		setup      *ast.FuncDecl
		recvName   = "s"
		helpers    int
		tests      int
	)
	for _, p := range pkg {
		for _, decl := range p.ast.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				if d.Tok != token.TYPE || len(d.Specs) != 1 {
					continue
				}
				if spec := d.Specs[0].(*ast.TypeSpec); spec.Name.Name == suiteType {
					typeDecl, typeFile = d, p.file.Path
				}
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				if d.Recv == nil {
					for _, t := range suitesRunBy(d) {
						if t == suiteType {
							runner, runnerName = d, d.Name.Name
						}
					}
					continue
				}
				if receiverType(d) != suiteType {
					continue
				}
				switch {
				case d.Name.Name == "SetupSuite":
					setup = d
					if name := receiverName(d); name != "" {
						recvName = name
					}
				case isTestFuncName(d.Name.Name) && len(d.Type.Params.List) == 0:
					if receiverName(d) == "" {
						return nil, fmt.Errorf("%s: %s.%s has no receiver name", p.file.Path, suiteType, d.Name.Name)
					}
					tests++
				case suiteHooks[d.Name.Name]:
					return nil, fmt.Errorf("%s: %s has the suite hook %s, which cannot be converted mechanically", p.file.Path, suiteType, d.Name.Name)
				default:
					helpers++
				}
			}
		}
	}
	firstPath := ""
	if len(files) > 0 {
		firstPath = files[0].Path
	}
	if typeDecl == nil {
		return nil, fmt.Errorf("%s: suite type %s not found", firstPath, suiteType)
	}
	if runner == nil {
		return nil, fmt.Errorf("%s: no test function runs %s with suite.Run", typeFile, suiteType)
	}
	if tests == 0 {
		return nil, fmt.Errorf("%s: %s has no test methods", typeFile, suiteType)
	}

	baseName := strings.TrimSuffix(suiteType, "Suite")
	ctor := "new" + baseName + "Test"
	// The type that replaces the suite type wherever it is still named: the
	// receivers of helper methods and the parameters of helper functions.
	newType := "fixtures.Test"
	newValue := "fixtures.New(t)"
	if helpers > 0 {
		newType = lowerCamel(baseName) + "Test"
		newValue = fmt.Sprintf("&%s{fixtures.New(t)}", newType)
	}

	// Second pass: edit each file.
	out := map[string][]byte{}
	for _, p := range pkg {
		src := p.file.Src
		type edit struct {
			start, end int
			text       string
		}
		var edits []edit
		// declRange covers a declaration with the blank line after it.
		declRange := func(start, end token.Pos) (int, int) {
			e := offset(end)
			for e < len(src) && src[e] == '\n' {
				e++
			}
			return offset(start), e
		}
		docStart := func(doc *ast.CommentGroup, pos token.Pos) token.Pos {
			if doc != nil {
				return doc.Pos()
			}
			return pos
		}

		for _, decl := range p.ast.Decls {
			switch {
			case decl == ast.Decl(typeDecl):
				// The constructor takes the place of the suite type declaration.
				var setupBody string
				if setup != nil {
					stmts := setup.Body.List
					if len(stmts) == 0 || !isEmbeddedSetupCall(stmts[0], recvName) {
						return nil, fmt.Errorf("%s: %s.SetupSuite does not start with %s.E2ESuite.SetupSuite()", fset.Position(setup.Pos()).Filename, suiteType, recvName)
					}
					// SetupSuite may be declared in another file than the suite type.
					setupSrc := srcByPath[fset.Position(setup.Pos()).Filename]
					setupBody = strings.TrimRight(string(setupSrc[offset(stmts[0].End()):offset(setup.Body.Rbrace)]), " \t\n")
				}
				var sb strings.Builder
				if helpers > 0 {
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
				s, e := declRange(docStart(typeDecl.Doc, typeDecl.Pos()), typeDecl.End())
				edits = append(edits, edit{start: s, end: e, text: sb.String()})
				continue
			case decl == ast.Decl(runner), setup != nil && decl == ast.Decl(setup):
				fn := decl.(*ast.FuncDecl)
				s, e := declRange(docStart(fn.Doc, fn.Pos()), fn.End())
				edits = append(edits, edit{start: s, end: e})
				continue
			}

			fn, isFunc := decl.(*ast.FuncDecl)
			isTest := isFunc && fn.Recv != nil && receiverType(fn) == suiteType &&
				isTestFuncName(fn.Name.Name) && len(fn.Type.Params.List) == 0
			if isTest {
				name := fn.Name.Name
				if r, ok := renames[runnerName+"/"+name]; ok {
					name = r
				}
				// fn.Pos() is the "func" keyword, so a doc comment is kept as it is.
				edits = append(edits, edit{
					start: offset(fn.Pos()),
					end:   offset(fn.Body.Lbrace) + 1,
					text:  fmt.Sprintf("func %s(t *testing.T) {\n\t%s := %s(t)", name, receiverName(fn), ctor),
				})
			}
			// Retarget every remaining mention of *SuiteType.
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
		if len(edits) == 0 {
			continue
		}

		sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
		text := append([]byte(nil), src...)
		for _, ed := range edits {
			text = append(text[:ed.start], append([]byte(ed.text), text[ed.end:]...)...)
		}
		// Removing the last declaration of a file leaves the blank line that
		// was in front of it.
		text = append(bytes.TrimRight(text, "\n"), '\n')
		// Drop the testify suite import once nothing refers to it.
		if !suiteUse.MatchString(stripImports(string(text))) {
			text = []byte(dropImport(string(text), "github.com/stretchr/testify/suite"))
		}

		// The file is deliberately not passed through gofmt: that would
		// reformat test bodies in files that were not gofmt-clean to begin
		// with. A file that was clean must still be clean afterwards.
		formatted, err := format.Source(text)
		if err != nil {
			return nil, fmt.Errorf("%s: converted source does not parse: %w", p.file.Path, err)
		}
		if wasClean, err := format.Source(src); err == nil && string(wasClean) == string(src) && string(formatted) != string(text) {
			return nil, fmt.Errorf("%s: the conversion left a gofmt-clean file unformatted:\n%s", p.file.Path, lineDiff(string(text), string(formatted), 2))
		}
		out[p.file.Path] = text
	}
	return out, nil
}

// dropImport removes one import line from the import block, and the blank
// line it would otherwise leave doubled or dangling.
func dropImport(src, importPath string) string {
	start := strings.Index(src, "import (")
	if start < 0 {
		return src
	}
	end := start + strings.Index(src[start:], "\n)")
	if end < start {
		return src
	}
	block := src[start : end+2]
	line := "\t\"" + importPath + "\"\n"
	if !strings.Contains(block, line) {
		return src
	}
	block = strings.Replace(block, line, "", 1)
	block = strings.Replace(block, "\n\n\n", "\n\n", 1)
	block = strings.Replace(block, "(\n\n", "(\n", 1)
	block = strings.Replace(block, "\n\n)", "\n)", 1)
	return src[:start] + block + src[end+2:]
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
