//go:build ignore

// Source-level evidence reader: syntax-aware (not execution coverage).
// Invoke: go run scripts/reuse_evidence.go < JSON payload
package main

import (
 "encoding/json"
 "fmt"
 "go/ast"
 "go/parser"
 "go/token"
 "os"
 "strconv"
 "strings"
)

type Input struct { Files map[string]string `json:"files"` }
type FileEvidence struct {
 Functions map[string]bool `json:"functions"`
 Calls []string `json:"calls"`
 ScopedCalls map[string][]string `json:"scoped_calls"`
 Imports map[string]string `json:"imports"`
}
type Output struct { Files map[string]FileEvidence `json:"files"` }

func main() {
 var in Input
 if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil { fatal(err) }
 out := Output{Files: make(map[string]FileEvidence)}
 for path, source := range in.Files {
  f, err := parser.ParseFile(token.NewFileSet(), path, source, parser.AllErrors)
  if err != nil { fatal(fmt.Errorf("%s: %w", path, err)) }
  ev := FileEvidence{Functions: make(map[string]bool), Calls: []string{}, ScopedCalls: make(map[string][]string), Imports: make(map[string]string)}
  for _, imp := range f.Imports {
   pkg, _ := strconv.Unquote(imp.Path.Value)
   alias := pkg[strings.LastIndex(pkg, "/")+1:]
   if imp.Name != nil {alias = imp.Name.Name}
   ev.Imports[alias] = pkg
  }
  for _, decl := range f.Decls {
   fn, ok := decl.(*ast.FuncDecl)
   if !ok || fn.Body == nil { continue }
   // A policy must calculate, decide, or mutate; an empty function, constant
   // return, or a forwarding-only stub cannot establish ownership.
   meaningful := false
   ast.Inspect(fn.Body, func(n ast.Node) bool {
    switch n.(type) {
    case *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt,
         *ast.SelectStmt, *ast.ForStmt, *ast.RangeStmt,
         *ast.BinaryExpr, *ast.UnaryExpr, *ast.CompositeLit,
         *ast.AssignStmt, *ast.IncDecStmt:
     meaningful = true
    case *ast.CallExpr:
     c := n.(*ast.CallExpr)
     switch f := c.Fun.(type) {
     case *ast.SelectorExpr:
      if receiver, ok := f.X.(*ast.Ident); ok {
       ev.Calls = append(ev.Calls, receiver.Name+"."+f.Sel.Name)
       ev.ScopedCalls[fn.Name.Name] = append(ev.ScopedCalls[fn.Name.Name], receiver.Name+"."+f.Sel.Name)
      }
     case *ast.Ident:
      ev.Calls = append(ev.Calls, f.Name)
      ev.ScopedCalls[fn.Name.Name] = append(ev.ScopedCalls[fn.Name.Name], f.Name)
     }
    }
    return true
   })
   ev.Functions[fn.Name.Name] = meaningful
  }
  out.Files[path] = ev
 }
 if err := json.NewEncoder(os.Stdout).Encode(out); err != nil { fatal(err) }
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
