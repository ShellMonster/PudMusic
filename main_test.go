package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestMainDelegatesApplicationAssemblyToBootstrap(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	var mainFn *ast.FuncDecl
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" {
			mainFn = fn
			break
		}
	}
	if mainFn == nil {
		t.Fatal("main.go has no func main")
	}

	var calls []string
	ast.Inspect(mainFn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok {
			calls = append(calls, pkg.Name+"."+sel.Sel.Name)
		}
		return true
	})

	if !containsCall(calls, "bootstrap.Assemble") {
		t.Fatalf("main does not delegate to bootstrap.Assemble: %v", calls)
	}
	if !containsCall(calls, "bootstrap.DefaultDependencies") {
		t.Fatalf("main does not use production bootstrap dependencies: %v", calls)
	}
	for _, name := range calls {
		if name == "app.New" || name == "platform.PinUserDataDir" || name == "ui.MountSettings" || name == "ui.Run" {
			t.Fatalf("main owns application assembly call %s; keep assembly in bootstrap: %v", name, calls)
		}
	}
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
