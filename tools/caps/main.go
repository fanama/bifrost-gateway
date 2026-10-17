// Command caps enforces the repository readability limits:
//   - at most 300 lines per Go file;
//   - at most 40 lines per function;
//   - at most 3 nested control-flow levels per function.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type violation struct {
	file string
	line int
	msg  string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	var found []violation
	files := 0
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "/vendor/") || strings.Contains(path, "/.git/") || strings.HasPrefix(path, "tools/caps/") {
			return nil
		}
		files++
		found = append(found, checkFile(path)...)
		return nil
	})
	if walkErr != nil {
		fmt.Fprintln(os.Stderr, walkErr)
		os.Exit(2)
	}
	if len(found) == 0 {
		fmt.Printf("caps ok: %d files\n", files)
		return
	}
	for _, v := range found {
		fmt.Printf("%s:%d: %s\n", v.file, v.line, v.msg)
	}
	os.Exit(1)
}

func checkFile(path string) []violation {
	src, err := os.ReadFile(path)
	if err != nil {
		return []violation{{path, 0, err.Error()}}
	}
	var out []violation
	if lines := strings.Count(string(src), "\n") + 1; lines > 300 {
		out = append(out, violation{path, 1, fmt.Sprintf("file has %d lines (max 300)", lines)})
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return append(out, violation{path, 0, err.Error()})
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Pos()).Line
		end := fset.Position(fn.End()).Line
		if lines := end - start + 1; lines > 40 {
			out = append(out, violation{path, start, fmt.Sprintf("func %s has %d lines (max 40)", fn.Name.Name, lines)})
		}
		if depth := maxDepth(fn.Body.List, 0); depth > 3 {
			out = append(out, violation{path, start, fmt.Sprintf("func %s nests %d levels (max 3)", fn.Name.Name, depth)})
		}
	}
	return out
}

// maxDepth returns the deepest control-flow nesting inside a statement list.
func maxDepth(stmts []ast.Stmt, depth int) int {
	max := depth
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.IfStmt:
			max = maxInt(max, maxDepth(s.Body.List, depth+1))
			if s.Else != nil {
				if block, ok := s.Else.(*ast.BlockStmt); ok {
					max = maxInt(max, maxDepth(block.List, depth+1))
				}
			}
		case *ast.ForStmt:
			max = maxInt(max, maxDepth(s.Body.List, depth+1))
		case *ast.RangeStmt:
			max = maxInt(max, maxDepth(s.Body.List, depth+1))
		case *ast.SwitchStmt:
			max = maxInt(max, maxDepth(s.Body.List, depth+1))
		case *ast.TypeSwitchStmt:
			max = maxInt(max, maxDepth(s.Body.List, depth+1))
		case *ast.SelectStmt:
			max = maxInt(max, maxDepth(s.Body.List, depth+1))
		case *ast.BlockStmt:
			max = maxInt(max, maxDepth(s.List, depth+1))
		}
	}
	return max
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
