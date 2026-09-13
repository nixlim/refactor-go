// Package decls provides shared helpers for listing and hashing top-level Go declarations.
package decls

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Decl is one top-level declaration (or one spec of a grouped var/const/type decl).
type Decl struct {
	Name     string // "Foo", "Type.Method", "T" ; grouped specs share the same GenDecl
	Kind     string // func, method, type, var, const, init
	Start    int    // byte offset including doc comment
	End      int    // byte offset exclusive
	Line     int
	EndLine  int
	Exported bool
	Node     ast.Node
	GenDecl  *ast.GenDecl // non-nil for var/const/type
	Group    int          // index of the GenDecl group (specs of one `var ( ... )` share it)
	Refs     []string     // top-level names referenced from this decl (filled by caller)
}

var SkipDirs = map[string]bool{"vendor": true, ".git": true, "node_modules": true, "testdata": true}

func recvTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	}
	return "?"
}

// List returns every top-level declaration in file f.
func List(fset *token.FileSet, f *ast.File, src []byte) []Decl {
	var out []Decl
	group := 0
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			name, kind := d.Name.Name, "func"
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = recvTypeName(d.Recv.List[0].Type) + "." + name
				kind = "method"
			}
			if name == "init" {
				kind = "init"
			}
			start := d.Pos()
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			out = append(out, Decl{Name: name, Kind: kind, Start: fset.Position(start).Offset, End: fset.Position(d.End()).Offset,
				Line: fset.Position(d.Pos()).Line, EndLine: fset.Position(d.End()).Line, Exported: ast.IsExported(d.Name.Name), Node: d})
		case *ast.GenDecl:
			group++
			kind := map[token.Token]string{token.TYPE: "type", token.VAR: "var", token.CONST: "const", token.IMPORT: "import"}[d.Tok]
			if kind == "import" {
				continue
			}
			start := d.Pos()
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					out = append(out, Decl{Name: s.Name.Name, Kind: kind, Start: fset.Position(start).Offset, End: fset.Position(d.End()).Offset,
						Line: fset.Position(s.Pos()).Line, EndLine: fset.Position(s.End()).Line, Exported: ast.IsExported(s.Name.Name), Node: s, GenDecl: d, Group: group})
				case *ast.ValueSpec:
					for _, n := range s.Names {
						out = append(out, Decl{Name: n.Name, Kind: kind, Start: fset.Position(start).Offset, End: fset.Position(d.End()).Offset,
							Line: fset.Position(s.Pos()).Line, EndLine: fset.Position(s.End()).Line, Exported: ast.IsExported(n.Name), Node: s, GenDecl: d, Group: group})
					}
				}
			}
		}
	}
	return out
}

// Hash prints the declaration node without comments and hashes it.
func Hash(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.RawFormat}
	_ = cfg.Fprint(&buf, fset, n)
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])[:16]
}

// RefsWithin returns the names in `names` referenced by node n (identifier match).
func RefsWithin(n ast.Node, names map[string]bool, self string) []string {
	seen := map[string]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok && names[id.Name] && id.Name != self {
			seen[id.Name] = true
		}
		return true
	})
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GoFiles walks paths (files or dirs) and yields .go files, skipping vendor/testdata.
func GoFiles(paths []string, includeTests bool) []string {
	var out []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			if strings.HasSuffix(p, ".go") {
				out = append(out, p)
			}
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if SkipDirs[d.Name()] && path != p {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && (includeTests || !strings.HasSuffix(path, "_test.go")) {
				out = append(out, path)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}
