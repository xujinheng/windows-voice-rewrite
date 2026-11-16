package contractgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

type kind string

const (
	kindInt    kind = "int"
	kindDouble kind = "double"
	kindString kind = "string"
	kindBool   kind = "bool"
	kindDate   kind = "date"
	kindPhase  kind = "phase"
	kindUint8  kind = "uint8"
	kindUint32 kind = "uint32"
	kindOther  kind = "other"
)

type conv struct {
	names     map[string]string
	rename    map[string]string
	dropRecv  string
	phases    map[string]bool
	constKind map[string]kind
	locals    map[string]kind
	declared  map[string]bool
	assigns   map[string]int
}

func parseFile(path string) (*ast.File, error) {
	return parser.ParseFile(token.NewFileSet(), path, nil, 0)
}

func (c *conv) fn(name string, file *ast.File) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func (c *conv) prepare(fn *ast.FuncDecl) {
	c.locals = map[string]kind{}
	c.declared = map[string]bool{}
	c.assigns = map[string]int{}
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			k := c.paramKind(field.Type)
			for _, name := range field.Names {
				c.locals[name.Name] = k
				c.declared[name.Name] = true
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if ok && id.Name != "_" {
				c.assigns[id.Name]++
			}
		}
		return true
	})
}

func (c *conv) paramKind(e ast.Expr) kind {
	switch t := e.(type) {
	case *ast.Ident:
		switch t.Name {
		case "int", "int32":
			return kindInt
		case "string":
			return kindString
		case "bool":
			return kindBool
		case "float64":
			return kindDouble
		case "uint8":
			return kindUint8
		case "uint32":
			return kindUint32
		case "Phase":
			return kindPhase
		}
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok && id.Name == "time" && t.Sel.Name == "Time" {
			return kindDate
		}
	}
	return kindOther
}

func (c *conv) block(body *ast.BlockStmt, indent int) (string, error) {
	var b strings.Builder
	for _, stmt := range body.List {
		line, err := c.stmt(stmt, indent)
		if err != nil {
			return "", err
		}
		if line == "" {
			continue
		}
		b.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

func (c *conv) stmt(stmt ast.Stmt, indent int) (string, error) {
	pad := strings.Repeat("    ", indent)
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		inner, err := c.block(s, indent)
		if err != nil {
			return "", err
		}
		return inner, nil
	case *ast.ReturnStmt:
		if len(s.Results) == 0 {
			return pad + "return", nil
		}
		parts := make([]string, len(s.Results))
		for i, result := range s.Results {
			text, _, err := c.expr(result)
			if err != nil {
				return "", err
			}
			parts[i] = text
		}
		if len(parts) == 1 {
			return pad + "return " + parts[0], nil
		}
		return pad + "return (" + strings.Join(parts, ", ") + ")", nil
	case *ast.IfStmt:
		return c.ifStmt(s, indent)
	case *ast.SwitchStmt:
		return c.switchStmt(s, indent)
	case *ast.AssignStmt:
		return c.assign(s, indent)
	case *ast.IncDecStmt:
		id, ok := s.X.(*ast.Ident)
		if !ok {
			return "", fmt.Errorf("incdec")
		}
		op := " += 1"
		if s.Tok == token.DEC {
			op = " -= 1"
		}
		return pad + id.Name + op, nil
	case *ast.ExprStmt:
		text, _, err := c.expr(s.X)
		if err != nil {
			return "", err
		}
		if text == "" {
			return "", nil
		}
		return pad + text, nil
	case *ast.DeferStmt:
		return "", nil
	case *ast.BranchStmt:
		if s.Tok == token.BREAK {
			return pad + "break", nil
		}
		return "", fmt.Errorf("branch %s", s.Tok)
	case *ast.RangeStmt:
		return c.rangeStmt(s, indent)
	case *ast.ForStmt:
		return c.forStmt(s, indent)
	default:
		return "", fmt.Errorf("stmt %T", stmt)
	}
}

func (c *conv) ifStmt(s *ast.IfStmt, indent int) (string, error) {
	pad := strings.Repeat("    ", indent)
	var b strings.Builder
	if s.Init != nil {
		line, err := c.stmt(s.Init, indent)
		if err != nil {
			return "", err
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	cond, _, err := c.expr(s.Cond)
	if err != nil {
		return "", err
	}
	b.WriteString(pad + "if " + cond + " {\n")
	inner, err := c.block(s.Body, indent+1)
	if err != nil {
		return "", err
	}
	b.WriteString(inner)
	if s.Else != nil {
		b.WriteString(pad + "} else {\n")
		elseLine, err := c.stmt(s.Else, indent+1)
		if err != nil {
			return "", err
		}
		b.WriteString(elseLine)
		if !strings.HasSuffix(elseLine, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString(pad + "}")
	return b.String(), nil
}

func (c *conv) switchStmt(s *ast.SwitchStmt, indent int) (string, error) {
	pad := strings.Repeat("    ", indent)
	var b strings.Builder
	if s.Init != nil {
		line, err := c.stmt(s.Init, indent)
		if err != nil {
			return "", err
		}
		b.WriteString(line + "\n")
	}
	if s.Tag == nil {
		for i, clause := range s.Body.List {
			cc := clause.(*ast.CaseClause)
			if cc.List == nil {
				return "", fmt.Errorf("tagless default")
			}
			cond, _, err := c.expr(cc.List[0])
			if err != nil {
				return "", err
			}
			word := "if"
			if i > 0 {
				word = "} else if"
			}
			b.WriteString(pad + word + " " + cond + " {\n")
			for _, st := range cc.Body {
				line, err := c.stmt(st, indent+1)
				if err != nil {
					return "", err
				}
				b.WriteString(line + "\n")
			}
		}
		b.WriteString(pad + "}")
		return b.String(), nil
	}
	tag, _, err := c.expr(s.Tag)
	if err != nil {
		return "", err
	}
	b.WriteString(pad + "switch " + tag + " {\n")
	for _, clause := range s.Body.List {
		cc := clause.(*ast.CaseClause)
		if cc.List == nil {
			b.WriteString(pad + "default:\n")
		} else {
			labels := make([]string, len(cc.List))
			for i, item := range cc.List {
				text, _, err := c.expr(item)
				if err != nil {
					return "", err
				}
				labels[i] = text
			}
			b.WriteString(pad + "case " + strings.Join(labels, ", ") + ":\n")
		}
		for _, st := range cc.Body {
			line, err := c.stmt(st, indent+1)
			if err != nil {
				return "", err
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString(pad + "}")
	return b.String(), nil
}

func (c *conv) assign(s *ast.AssignStmt, indent int) (string, error) {
	pad := strings.Repeat("    ", indent)
	if len(s.Lhs) > 1 && len(s.Lhs) == len(s.Rhs) {
		var b strings.Builder
		for i := range s.Lhs {
			line, err := c.assign(&ast.AssignStmt{Lhs: []ast.Expr{s.Lhs[i]}, Tok: s.Tok, Rhs: []ast.Expr{s.Rhs[i]}}, indent)
			if err != nil {
				return "", err
			}
			b.WriteString(line)
			if i != len(s.Lhs)-1 {
				b.WriteByte('\n')
			}
		}
		return b.String(), nil
	}
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return "", fmt.Errorf("multi assign")
	}
	id, ok := s.Lhs[0].(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("assign lhs %T", s.Lhs[0])
	}
	right, k, err := c.expr(s.Rhs[0])
	if err != nil {
		return "", err
	}
	name := c.localName(id.Name)
	if c.declared[id.Name] {
		return pad + name + " = " + right, nil
	}
	c.declared[id.Name] = true
	c.locals[id.Name] = k
	word := "let"
	if c.assigns[id.Name] > 1 {
		word = "var"
	}
	return pad + word + " " + name + " = " + right, nil
}

func (c *conv) localName(goName string) string {
	if c.rename != nil {
		if name, ok := c.rename[goName]; ok {
			return name
		}
	}
	return goName
}

func (c *conv) paramCopies(fn *ast.FuncDecl, indent int) string {
	if fn == nil || fn.Type.Params == nil {
		return ""
	}
	pad := strings.Repeat("    ", indent)
	var b strings.Builder
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if c.assigns[name.Name] == 0 {
				continue
			}
			swift := c.localName(name.Name)
			b.WriteString(pad + "var " + swift + " = " + swift + "\n")
		}
	}
	return b.String()
}

func (c *conv) rangeStmt(s *ast.RangeStmt, indent int) (string, error) {
	pad := strings.Repeat("    ", indent)
	src, _, err := c.expr(s.X)
	if err != nil {
		return "", err
	}
	value := "_"
	if id, ok := s.Value.(*ast.Ident); ok {
		value = id.Name
	}
	body, err := c.block(s.Body, indent+1)
	if err != nil {
		return "", err
	}
	return pad + "for " + value + " in " + src + " {\n" + body + pad + "}", nil
}

func (c *conv) forStmt(s *ast.ForStmt, indent int) (string, error) {
	if s.Init == nil && s.Post == nil && s.Cond != nil {
		cond, _, err := c.expr(s.Cond)
		if err != nil {
			return "", err
		}
		body, err := c.block(s.Body, indent+1)
		if err != nil {
			return "", err
		}
		pad := strings.Repeat("    ", indent)
		return pad + "while " + cond + " {\n" + body + pad + "}", nil
	}
	return "", fmt.Errorf("for")
}

func (c *conv) expr(e ast.Expr) (string, kind, error) {
	switch n := e.(type) {
	case *ast.Ident:
		return c.ident(n)
	case *ast.BasicLit:
		return c.lit(n)
	case *ast.ParenExpr:
		text, k, err := c.expr(n.X)
		if err != nil {
			return "", "", err
		}
		return "(" + text + ")", k, nil
	case *ast.UnaryExpr:
		text, k, err := c.expr(n.X)
		if err != nil {
			return "", "", err
		}
		return n.Op.String() + text, k, nil
	case *ast.BinaryExpr:
		left, lk, err := c.expr(n.X)
		if err != nil {
			return "", "", err
		}
		right, rk, err := c.expr(n.Y)
		if err != nil {
			return "", "", err
		}
		k := lk
		if n.Op == token.EQL || n.Op == token.NEQ || n.Op == token.LSS || n.Op == token.GTR || n.Op == token.LEQ || n.Op == token.GEQ || n.Op == token.LAND || n.Op == token.LOR {
			k = kindBool
		} else if rk == kindDouble || lk == kindDouble {
			k = kindDouble
		}
		return "(" + left + " " + n.Op.String() + " " + right + ")", k, nil
	case *ast.CallExpr:
		return c.call(n)
	case *ast.SelectorExpr:
		return c.selector(n)
	case *ast.IndexExpr:
		recv, _, err := c.expr(n.X)
		if err != nil {
			return "", "", err
		}
		idx, _, err := c.expr(n.Index)
		if err != nil {
			return "", "", err
		}
		return recv + "[" + idx + "]", kindOther, nil
	default:
		return "", "", fmt.Errorf("expr %T", e)
	}
}

func (c *conv) ident(n *ast.Ident) (string, kind, error) {
	if n.Name == "nil" {
		return "nil", kindOther, nil
	}
	if c.rename != nil {
		if swift, ok := c.rename[n.Name]; ok {
			return swift, c.locals[n.Name], nil
		}
	}
	if c.phases[n.Name] {
		return "." + lowerCamel(n.Name), kindPhase, nil
	}
	if k, ok := c.constKind[n.Name]; ok {
		return c.names[n.Name], k, nil
	}
	if swift, ok := c.names[n.Name]; ok {
		return swift, c.locals[n.Name], nil
	}
	if k, ok := c.locals[n.Name]; ok {
		return n.Name, k, nil
	}
	return n.Name, kindOther, nil
}

func (c *conv) lit(n *ast.BasicLit) (string, kind, error) {
	switch n.Kind {
	case token.STRING:
		return n.Value, kindString, nil
	case token.INT:
		return n.Value, kindInt, nil
	case token.FLOAT:
		return n.Value, kindDouble, nil
	default:
		return "", "", fmt.Errorf("lit %s", n.Kind)
	}
}

func (c *conv) selector(n *ast.SelectorExpr) (string, kind, error) {
	if id, ok := n.X.(*ast.Ident); ok && id.Name == c.dropRecv && c.dropRecv != "" {
		return lowerCamel(n.Sel.Name), kindInt, nil
	}
	if id, ok := n.X.(*ast.Ident); ok && id.Name == "math" && n.Sel.Name == "Pi" {
		return "Double.pi", kindDouble, nil
	}
	if id, ok := n.X.(*ast.Ident); ok && id.Name == "time" && n.Sel.Name == "Millisecond" {
		return "", "", fmt.Errorf("bare millisecond")
	}
	recv, k, err := c.expr(n.X)
	if err != nil {
		return "", "", err
	}
	switch n.Sel.Name {
	case "Seconds":
		return recv, kindDouble, nil
	default:
		return recv + "." + lowerCamel(n.Sel.Name), k, nil
	}
}

func (c *conv) call(n *ast.CallExpr) (string, kind, error) {
	switch fun := n.Fun.(type) {
	case *ast.Ident:
		switch fun.Name {
		case "float64":
			inner, k, err := c.expr(n.Args[0])
			if err != nil {
				return "", "", err
			}
			if k == kindInt || k == kindUint32 || k == kindUint8 {
				return "Double(" + inner + ")", kindDouble, nil
			}
			return inner, kindDouble, nil
		case "int", "int32":
			inner, _, err := c.expr(n.Args[0])
			if err != nil {
				return "", "", err
			}
			return "Int(" + inner + ")", kindInt, nil
		case "uint8":
			return c.uint8(n.Args[0])
		case "len":
			inner, k, err := c.expr(n.Args[0])
			if err != nil {
				return "", "", err
			}
			if k == kindString {
				return inner + ".utf8.count", kindInt, nil
			}
			return inner + ".count", kindInt, nil
		}
		if sig, ok := c.names[fun.Name]; ok && !c.phases[fun.Name] {
			return c.callNamed(fun.Name, sig, n.Args)
		}
		return "", "", fmt.Errorf("call %s", fun.Name)
	case *ast.SelectorExpr:
		return c.selCall(fun, n.Args)
	default:
		return "", "", fmt.Errorf("call fun %T", n.Fun)
	}
}

func (c *conv) callNamed(goName, swift string, args []ast.Expr) (string, kind, error) {
	sig, ok := signatures[goName]
	if !ok {
		return "", "", fmt.Errorf("no signature %s", goName)
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		text, _, err := c.expr(arg)
		if err != nil {
			return "", "", err
		}
		label := sig.params[i].external
		if label == "_" {
			parts[i] = text
		} else {
			if label == "" {
				label = sig.params[i].goName
			}
			parts[i] = label + ": " + text
		}
	}
	return swift + "(" + strings.Join(parts, ", ") + ")", sig.result, nil
}

func (c *conv) uint8(arg ast.Expr) (string, kind, error) {
	if call, ok := arg.(*ast.CallExpr); ok {
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "int" {
			return "", "", fmt.Errorf("uint8 int")
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Round" {
			inner, _, err := c.expr(call.Args[0])
			if err != nil {
				return "", "", err
			}
			return "UInt8((" + inner + ").rounded())", kindUint8, nil
		}
	}
	inner, k, err := c.expr(arg)
	if err != nil {
		return "", "", err
	}
	if k == kindDouble {
		return "UInt8((" + inner + ").rounded())", kindUint8, nil
	}
	return "UInt8(" + inner + ")", kindUint8, nil
}

func (c *conv) selCall(fun *ast.SelectorExpr, args []ast.Expr) (string, kind, error) {
	if pkg, ok := fun.X.(*ast.Ident); ok {
		switch pkg.Name {
		case "math":
			return c.mathCall(fun.Sel.Name, args)
		case "fmt":
			return c.fmtCall(fun.Sel.Name, args)
		case "strings":
			return c.stringsCall(fun.Sel.Name, args)
		case "utf8":
			if fun.Sel.Name == "RuneCountInString" {
				inner, _, err := c.expr(args[0])
				if err != nil {
					return "", "", err
				}
				return inner + ".unicodeScalars.count", kindInt, nil
			}
		}
	}
	recv, _, err := c.expr(fun.X)
	if err != nil {
		return "", "", err
	}
	switch fun.Sel.Name {
	case "Seconds":
		return recv, kindDouble, nil
	case "Before":
		other, _, err := c.expr(args[0])
		if err != nil {
			return "", "", err
		}
		return recv + " < " + other, kindBool, nil
	case "Sub":
		other, _, err := c.expr(args[0])
		if err != nil {
			return "", "", err
		}
		return recv + ".timeIntervalSince(" + other + ")", kindDouble, nil
	case "Contains":
		x, _, err := c.expr(args[0])
		if err != nil {
			return "", "", err
		}
		y, _, err := c.expr(args[1])
		if err != nil {
			return "", "", err
		}
		return recv + ".contains(x: " + x + ", y: " + y + ")", kindBool, nil
	case "TrimSpace":
		return recv + ".trimmingCharacters(in: .whitespacesAndNewlines)", kindString, nil
	case "Lock", "Unlock":
		return "", kindOther, nil
	default:
		return "", "", fmt.Errorf("selector call %s", fun.Sel.Name)
	}
}

func (c *conv) mathCall(name string, args []ast.Expr) (string, kind, error) {
	inner := make([]string, len(args))
	for i, arg := range args {
		text, _, err := c.expr(arg)
		if err != nil {
			return "", "", err
		}
		inner[i] = text
	}
	switch name {
	case "Round":
		return "(" + inner[0] + ").rounded()", kindDouble, nil
	case "Cos":
		return "cos(" + inner[0] + ")", kindDouble, nil
	case "Sin":
		return "sin(" + inner[0] + ")", kindDouble, nil
	case "Abs":
		return "abs(" + inner[0] + ")", kindDouble, nil
	case "Pow":
		return "pow(" + inner[0] + ", " + inner[1] + ")", kindDouble, nil
	case "IsNaN":
		return inner[0] + ".isNaN", kindBool, nil
	case "IsInf":
		return inner[0] + ".isInfinite", kindBool, nil
	default:
		return "", "", fmt.Errorf("math %s", name)
	}
}

func (c *conv) fmtCall(name string, args []ast.Expr) (string, kind, error) {
	if name != "Sprintf" && name != "Errorf" {
		return "", "", fmt.Errorf("fmt %s", name)
	}
	format, _, err := c.expr(args[0])
	if err != nil {
		return "", "", err
	}
	parts := make([]string, len(args)-1)
	for i, arg := range args[1:] {
		text, _, err := c.expr(arg)
		if err != nil {
			return "", "", err
		}
		parts[i] = text
	}
	if len(parts) == 0 {
		return format, kindString, nil
	}
	return "String(format: " + format + ", " + strings.Join(parts, ", ") + ")", kindString, nil
}

func (c *conv) stringsCall(name string, args []ast.Expr) (string, kind, error) {
	switch name {
	case "TrimSpace":
		inner, _, err := c.expr(args[0])
		if err != nil {
			return "", "", err
		}
		return inner + ".trimmingCharacters(in: .whitespacesAndNewlines)", kindString, nil
	case "Contains":
		recv, _, err := c.expr(args[0])
		if err != nil {
			return "", "", err
		}
		needle, _, err := c.expr(args[1])
		if err != nil {
			return "", "", err
		}
		return recv + ".contains(" + needle + ")", kindBool, nil
	default:
		return "", "", fmt.Errorf("strings %s", name)
	}
}

func lowerCamel(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToLower(name[:1]) + name[1:]
}

func constNumber(expr ast.Expr) (string, bool) {
	switch n := expr.(type) {
	case *ast.BasicLit:
		return n.Value, true
	case *ast.BinaryExpr:
		if n.Op == token.SHL {
			left, ok := n.X.(*ast.BasicLit)
			right, ok2 := n.Y.(*ast.BasicLit)
			if ok && ok2 {
				return left.Value + " << " + right.Value, true
			}
		}
		if n.Op != token.MUL {
			return "", false
		}
		lit, ok := n.X.(*ast.BasicLit)
		if !ok || lit.Kind != token.INT {
			return "", false
		}
		sel, ok := n.Y.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Millisecond" {
			return "", false
		}
		ms, err := strconv.Atoi(lit.Value)
		if err != nil {
			return "", false
		}
		return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64), true
	default:
		return "", false
	}
}

func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	text, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return text, true
}
