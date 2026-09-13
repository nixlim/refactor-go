// snapshot records a hash of every top-level declaration under the given paths and
// compares later states against it. A pure relocation (same package or across packages)
// leaves every hash present; dropped code shows as MISSING, invented code as ADDED,
// edited code as CHANGED, and a decl that moved between packages as MOVED.
//
//	snapshot -out before.json ./...            (paths: dirs or files; ./... means .)
//	snapshot -compare before.json ./... [-strict] [-allow-changed A,B]
//
// Exit 0: identical or only MOVED; 1: MISSING/ADDED (or CHANGED with -strict); 2: usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"refactor-go/gotools/internal/decls"
)

type entry struct {
	Hash string `json:"hash"`
	File string `json:"file"`
}

func collect(paths []string) map[string]entry { // key: pkgdir::Name
	out := map[string]entry{}
	fset := token.NewFileSet()
	for _, p := range decls.GoFiles(paths, false) {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(fset, p, src, parser.ParseComments)
		if err != nil {
			out["__SYNTAX_ERROR__::"+p] = entry{Hash: "error", File: p}
			fmt.Fprintln(os.Stderr, "SYNTAX ERROR:", err)
			continue
		}
		pkgdir := filepath.ToSlash(filepath.Clean(filepath.Dir(p)))
		for _, d := range decls.List(fset, f, src) {
			key := pkgdir + "::" + d.Name
			if d.Kind == "init" { // several init() per package are legal; number them by file
				key = pkgdir + "::init@" + filepath.Base(p) + fmt.Sprintf("#%d", d.Line)
			}
			out[key] = entry{Hash: decls.Hash(fset, d.Node), File: p}
		}
	}
	return out
}

func main() {
	outPath := flag.String("out", "", "write snapshot JSON")
	cmpPath := flag.String("compare", "", "compare against snapshot JSON")
	strict := flag.Bool("strict", false, "fail on CHANGED too")
	allow := flag.String("allow-changed", "", "comma-separated names allowed to change")
	flag.Parse()
	paths := flag.Args()
	for i, p := range paths {
		paths[i] = strings.TrimSuffix(p, "/...")
		if paths[i] == "." || paths[i] == "" {
			paths[i] = "."
		}
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	if *outPath == "" && *cmpPath == "" {
		fmt.Fprintln(os.Stderr, "usage: snapshot -out before.json <paths> | snapshot -compare before.json <paths>")
		os.Exit(2)
	}
	now := collect(paths)
	if *outPath != "" {
		b, _ := json.MarshalIndent(now, "", " ")
		if err := os.WriteFile(*outPath, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("snapshot: %d decls -> %s\n", len(now), *outPath)
		return
	}
	b, err := os.ReadFile(*cmpPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var before map[string]entry
	if err := json.Unmarshal(b, &before); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	allowed := map[string]bool{}
	for _, a := range strings.Split(*allow, ",") {
		if a = strings.TrimSpace(a); a != "" {
			allowed[a] = true
		}
	}
	nameOf := func(key string) string { return key[strings.LastIndex(key, "::")+2:] }
	hashesNow := map[string][]string{}
	for k, e := range now {
		hashesNow[e.Hash] = append(hashesNow[e.Hash], k)
	}
	hashesBefore := map[string][]string{}
	for k, e := range before {
		hashesBefore[e.Hash] = append(hashesBefore[e.Hash], k)
	}
	var missing, added, changed, moved []string
	syntax := false
	for k, e := range before {
		if n, ok := now[k]; ok {
			if n.Hash != e.Hash && !allowed[nameOf(k)] {
				changed = append(changed, k+"  ("+n.File+")")
			}
			continue
		}
		if cands := hashesNow[e.Hash]; len(cands) > 0 { // same body elsewhere: cross-package move
			moved = append(moved, k+" -> "+cands[0])
			continue
		}
		missing = append(missing, k)
	}
	for k, e := range now {
		if strings.HasPrefix(k, "__SYNTAX_ERROR__") {
			syntax = true
			continue
		}
		if _, ok := before[k]; ok {
			continue
		}
		if len(hashesBefore[e.Hash]) > 0 {
			continue // counted as MOVED above
		}
		added = append(added, k+"  ("+e.File+")")
	}
	sort.Strings(missing)
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(moved)
	for _, m := range missing {
		fmt.Println("MISSING ", m)
	}
	for _, a := range added {
		fmt.Println("ADDED   ", a)
	}
	for _, c := range changed {
		fmt.Println("CHANGED ", c)
	}
	for _, m := range moved {
		fmt.Println("MOVED   ", m)
	}
	fmt.Printf("summary: %d before, %d after, missing=%d added=%d changed=%d moved=%d\n", len(before), len(now), len(missing), len(added), len(changed), len(moved))
	bad := len(missing) > 0 || len(added) > 0 || syntax || (*strict && len(changed) > 0)
	if len(changed) > 0 && !*strict {
		fmt.Println("note: CHANGED entries need reviewer sign-off (use -strict to fail on them)")
	}
	if bad {
		os.Exit(1)
	}
}
