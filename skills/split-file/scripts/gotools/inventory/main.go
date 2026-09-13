// inventory prints a Markdown map of a large Go file (and its package) so an agent
// can plan a split without reading the file.
//
//	inventory -file path/to/big.go [-json out.json] [-min-cluster-loc 150]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"refactor-go/gotools/internal/decls"
)

type symbol struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Line     int      `json:"line"`
	EndLine  int      `json:"end_line"`
	LOC      int      `json:"loc"`
	Exported bool     `json:"exported"`
	Refs     []string `json:"refs"`
	Mutable  bool     `json:"mutable_state"`
	Owner    string   `json:"owner_type,omitempty"` // for methods: receiver type
}

func isMutableInit(vs *ast.ValueSpec) bool {
	for _, v := range vs.Values {
		switch v.(type) {
		case *ast.CompositeLit, *ast.CallExpr, *ast.UnaryExpr:
			return true
		}
	}
	return false
}

func main() {
	file := flag.String("file", "", "Go file to inventory")
	jsonOut := flag.String("json", "", "write JSON here")
	minLOC := flag.Int("min-cluster-loc", 150, "only list clusters with >= this many lines")
	flag.Parse()
	if *file == "" {
		fmt.Fprintln(os.Stderr, "usage: inventory -file big.go [-json out.json]")
		os.Exit(2)
	}
	src, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, *file, src, parser.ParseComments)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse error:", err)
		os.Exit(1)
	}
	list := decls.List(fset, f, src)
	names := map[string]bool{}
	for _, d := range list {
		names[d.Name] = true
		if i := strings.Index(d.Name, "."); i > 0 { // methods are referenced via their type; also register bare method name
			names[d.Name[:i]] = true
		}
	}
	var syms []symbol
	byName := map[string]*symbol{}
	for i := range list {
		d := list[i]
		refs := decls.RefsWithin(d.Node, names, d.Name)
		s := symbol{Name: d.Name, Kind: d.Kind, Line: d.Line, EndLine: d.EndLine, LOC: d.EndLine - d.Line + 1, Exported: d.Exported, Refs: refs}
		if d.Kind == "method" {
			s.Owner = d.Name[:strings.Index(d.Name, ".")]
		}
		if vs, ok := d.Node.(*ast.ValueSpec); ok && d.Kind == "var" && isMutableInit(vs) {
			s.Mutable = true
		}
		syms = append(syms, s)
	}
	for i := range syms {
		byName[syms[i].Name] = &syms[i]
	}

	// union-find over funcs/types/methods; methods glued to their receiver type
	parent := map[string]string{}
	find := func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		if _, ok := parent[a]; !ok {
			return
		}
		if _, ok := parent[b]; !ok {
			return
		}
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for _, s := range syms {
		if s.Kind != "var" && s.Kind != "const" && s.Kind != "init" {
			parent[s.Name] = s.Name
		}
	}
	for _, s := range syms {
		if s.Owner != "" {
			union(s.Name, s.Owner)
		}
		for _, r := range s.Refs {
			union(s.Name, r)
		}
	}
	groups := map[string][]string{}
	for n := range parent {
		r := find(n)
		groups[r] = append(groups[r], n)
	}
	type cluster struct {
		Members []string `json:"members"`
		LOC     int      `json:"loc"`
	}
	var clusters []cluster
	for _, m := range groups {
		sort.Slice(m, func(i, j int) bool { return byName[m[i]].Line < byName[m[j]].Line })
		loc := 0
		for _, n := range m {
			loc += byName[n].LOC
		}
		clusters = append(clusters, cluster{Members: m, LOC: loc})
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].LOC > clusters[j].LOC })

	// package context: sibling files
	dir := filepath.Dir(*file)
	entries, _ := os.ReadDir(dir)
	var siblings []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && e.Name() != filepath.Base(*file) {
			siblings = append(siblings, e.Name())
		}
	}

	// ---- report
	fmt.Printf("# Inventory: %s (package %s)\n", *file, f.Name.Name)
	fmt.Printf("- total lines: %d\n", strings.Count(string(src), "\n")+1)
	counts := map[string]int{}
	for _, s := range syms {
		counts[s.Kind]++
	}
	fmt.Printf("- top-level decls: %d (func %d, method %d, type %d, var %d, const %d, init %d)\n", len(syms), counts["func"], counts["method"], counts["type"], counts["var"], counts["const"], counts["init"])
	fmt.Printf("- other files in package: %d (%s)\n", len(siblings), strings.Join(siblings, ", "))
	fmt.Println("- NOTE: moving decls between files of the SAME package needs no import or visibility changes.")
	fmt.Println()
	fmt.Println("## Package-level mutable state (one owning file; never duplicate)")
	foundState := false
	for _, s := range syms {
		if s.Mutable {
			foundState = true
			users := []string{}
			for _, t := range syms {
				for _, r := range t.Refs {
					if r == s.Name {
						users = append(users, t.Name)
					}
				}
			}
			fmt.Printf("- `%s` (line %d) used by: %s\n", s.Name, s.Line, strings.Join(users, ", "))
		}
	}
	if !foundState {
		fmt.Println("- none detected")
	}
	if counts["init"] > 0 {
		fmt.Println()
		fmt.Println("## init() functions (import-time side effects; keep order, keep in one file)")
		for _, s := range syms {
			if s.Kind == "init" {
				fmt.Printf("- init at line %d-%d\n", s.Line, s.EndLine)
			}
		}
	}
	fmt.Println()
	fmt.Printf("## Suggested clusters (types with their methods + connected funcs; >= %d LOC)\n", *minLOC)
	shown := 0
	for i, c := range clusters {
		if c.LOC < *minLOC && shown >= 3 {
			continue
		}
		shown++
		mem := c.Members
		if len(mem) > 40 {
			mem = append(append([]string{}, mem[:40]...), "...")
		}
		fmt.Printf("### Cluster %d: %d LOC, %d decls\n- members: %s\n", i+1, c.LOC, len(c.Members), strings.Join(mem, ", "))
	}
	fmt.Println()
	fmt.Println("## Largest decls")
	sorted := append([]symbol{}, syms...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].LOC > sorted[j].LOC })
	for i, s := range sorted {
		if i >= 15 {
			break
		}
		fmt.Printf("- `%s` (%s, lines %d-%d, %d LOC, fan-out %d)\n", s.Name, s.Kind, s.Line, s.EndLine, s.LOC, len(s.Refs))
	}
	fmt.Println()
	fmt.Println("## Leaves (no references to other decls in this file)")
	var leaves []string
	for _, s := range syms {
		if len(s.Refs) == 0 && (s.Kind == "func" || s.Kind == "type") {
			leaves = append(leaves, s.Name)
		}
	}
	fmt.Println(strings.Join(leaves, ", "))

	if *jsonOut != "" {
		data := map[string]any{"file": *file, "package": f.Name.Name, "symbols": syms, "clusters": clusters, "siblings": siblings}
		b, _ := json.MarshalIndent(data, "", " ")
		if err := os.WriteFile(*jsonOut, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "(json written to %s)\n", *jsonOut)
	}
}
