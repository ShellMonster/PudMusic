package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestMainDelegatesApplicationAssemblyToBootstrap(t *testing.T) {
	calls := functionCalls(t, "main.go", "main")
	if !containsCall(calls, "bootstrap.Assemble") || !containsCall(calls, "platformDependencies") {
		t.Fatalf("main does not delegate to bootstrap.Assemble with platform dependencies: %v", calls)
	}
	for file, want := range map[string]string{
		"entry_desktop.go": "bootstrap.DefaultDependencies",
		"entry_mobile.go":  "bootstrap.MobileDependencies",
	} {
		if calls := functionCalls(t, file, "platformDependencies"); !containsCall(calls, want) {
			t.Fatalf("%s does not use %s: %v", file, want, calls)
		}
	}
	for _, name := range calls {
		if name == "app.New" || name == "platform.PinUserDataDir" || name == "ui.MountSettings" || name == "ui.Run" {
			t.Fatalf("main owns application assembly call %s; keep assembly in bootstrap: %v", name, calls)
		}
	}
}

func functionCalls(t *testing.T, path, name string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var body *ast.BlockStmt
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			body = fn.Body
			break
		}
	}
	if body == nil {
		t.Fatalf("%s has no func %s", path, name)
	}

	var calls []string
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			calls = append(calls, id.Name)
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

	return calls
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
