// movedecl moves top-level declarations (with their doc comments) from one Go file to
// another, deterministically. It is the only sanctioned way for an agent to relocate Go
// code during a split. It never edits bodies.
//
//	movedecl -file src.go -dest dst.go -names Foo,Bar.Baz,MyType [-apply] [-package name]
//
// Names: "Foo" (func/type/var/const), "Type.Method" (method). A grouped declaration
// (var ( a = 1; b = 2 )) is moved only if every name in the group is listed.
//
// Same package: destination gets `package <same>` and no import fixes are needed except
// pruning/adding imports, which `goimports -w` does when present (run automatically).
// Different package (dest in another directory): the tool moves the text and reminds you
// that exported-ness and call sites must be handled with gopls rename; the compiler is
// the gate.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"refactor-go/gotools/internal/decls"
)

func main() {
	file := flag.String("file", "", "source .go file")
	dest := flag.String("dest", "", "destination .go file (created if missing)")
	names := flag.String("names", "", "comma-separated decl names, e.g. Foo,Bar.Baz")
	apply := flag.Bool("apply", false, "write changes (default: dry run)")
	pkgName := flag.String("package", "", "package name for a NEW destination file (default: same as source)")
	flag.Parse()
	if *file == "" || *dest == "" || *names == "" {
		fmt.Fprintln(os.Stderr, "usage: movedecl -file src.go -dest dst.go -names A,B,T.M [-apply]")
		os.Exit(2)
	}
	src, err := os.ReadFile(*file)
	if err != nil {
		fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, *file, src, parser.ParseComments)
	if err != nil {
		fatal(fmt.Errorf("parse %s: %w", *file, err))
	}
	list := decls.List(fset, f, src)
	want := map[string]bool{}
	for _, n := range strings.Split(*names, ",") {
		if n = strings.TrimSpace(n); n != "" {
			want[n] = true
		}
	}
	// group handling: a GenDecl group is moved as one unit only if all its names are wanted
	groupNames := map[int][]string{}
	for _, d := range list {
		if d.GenDecl != nil {
			groupNames[d.Group] = append(groupNames[d.Group], d.Name)
		}
	}
	type span struct{ start, end int; label string }
	var spans []span
	seenGroup := map[int]bool{}
	found := map[string]bool{}
	for _, d := range list {
		if !want[d.Name] {
			continue
		}
		found[d.Name] = true
		if d.GenDecl != nil {
			if seenGroup[d.Group] {
				continue
			}
			for _, gn := range groupNames[d.Group] {
				if !want[gn] {
					fatal(fmt.Errorf("%s is declared in a group with %s; list every name of the group (%s) or split the group first",
						d.Name, gn, strings.Join(groupNames[d.Group], ",")))
				}
			}
			seenGroup[d.Group] = true
			spans = append(spans, span{d.Start, d.End, strings.Join(groupNames[d.Group], ",")})
			continue
		}
		spans = append(spans, span{d.Start, d.End, d.Name})
	}
	for n := range want {
		if !found[n] {
			fatal(fmt.Errorf("decl %q not found at top level of %s", n, *file))
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	// extract texts in original order; cut from source in reverse order
	var moved [][]byte
	for _, s := range spans {
		moved = append(moved, bytes.TrimRight(src[s.start:s.end], "\n"))
	}
	out := append([]byte{}, src...)
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		end := s.end
		for end < len(out) && out[end] == '\n' {
			end++
		}
		out = append(out[:s.start], out[end:]...)
	}

	// destination
	var dst []byte
	samePkg := filepath.Clean(filepath.Dir(*file)) == filepath.Clean(filepath.Dir(*dest))
	if b, err := os.ReadFile(*dest); err == nil {
		dst = b
		if _, perr := parser.ParseFile(fset, *dest, b, parser.PackageClauseOnly); perr != nil {
			fatal(fmt.Errorf("destination exists but does not parse: %w", perr))
		}
	} else {
		pn := *pkgName
		if pn == "" {
			if samePkg {
				pn = f.Name.Name
			} else {
				pn = filepath.Base(filepath.Dir(*dest))
			}
		}
		dst = []byte(fmt.Sprintf("package %s\n", pn))
	}
	var dbuf bytes.Buffer
	dbuf.Write(bytes.TrimRight(dst, "\n"))
	for _, m := range moved {
		dbuf.WriteString("\n\n")
		dbuf.Write(m)
	}
	dbuf.WriteString("\n")

	srcFmt, err := format.Source(out)
	if err != nil {
		fatal(fmt.Errorf("source no longer formats after removal (should not happen): %w", err))
	}
	dstFmt, err := format.Source(dbuf.Bytes())
	if err != nil {
		fatal(fmt.Errorf("destination does not format: %w", err))
	}

	fmt.Printf("=== movedecl %s -> %s\n", *file, *dest)
	for _, s := range spans {
		fmt.Printf("move: %s (%d bytes)\n", s.label, s.end-s.start)
	}
	fmt.Printf("source: %d -> %d lines; destination: %d lines\n",
		bytes.Count(src, []byte("\n")), bytes.Count(srcFmt, []byte("\n")), bytes.Count(dstFmt, []byte("\n")))
	if !samePkg {
		fmt.Println("NOTE: destination is a different package. Unexported names moved across packages will not compile until exported and call sites updated (use gopls rename). The compiler is the gate.")
	}
	if !*apply {
		fmt.Println("(dry run; add -apply to write)")
		return
	}
	if err := os.MkdirAll(filepath.Dir(*dest), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*file, srcFmt, 0o644); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*dest, dstFmt, 0o644); err != nil {
		fatal(err)
	}
	if gi, err := exec.LookPath("goimports"); err == nil {
		cmd := exec.Command(gi, "-w", *file, *dest)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "goimports failed (fix imports manually, then gate):", err)
		}
	} else {
		fmt.Println("goimports not found: run `go build ./...` and fix unused/missing imports it reports")
	}
	fmt.Println("applied")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "MOVE FAILED:", err)
	os.Exit(1)
}
