package contractgen

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strconv"
	"strings"
)

func emitDiagnosticLog(swiftPath, goPath string) (string, error) {
	source, err := os.ReadFile(swiftPath)
	if err != nil {
		return "", err
	}
	file, err := parseFile(goPath)
	if err != nil {
		return "", err
	}
	fn := findFunc(file, "Rotate")
	body, err := emitRotateFunc(fn)
	if err != nil {
		return "", err
	}
	return replaceSwiftFunc(string(source), "rotate", body)
}

func emitRotateFunc(fn *ast.FuncDecl) (string, error) {
	if fn == nil || fn.Body == nil {
		return "", fmt.Errorf("missing Rotate")
	}
	var b strings.Builder
	b.WriteString("    // Source: voice-rewrite/internal/core/logfile/rotate.go\n")
	b.WriteString("    static func rotate(_ path: String, maxBytes: Int64, backups: Int) throws {\n")
	b.WriteString("        let manager = FileManager.default\n")
	text, err := emitRotateBlock(fn.Body.List, 2, false)
	if err != nil {
		return "", err
	}
	b.WriteString(text)
	b.WriteString("    }")
	return b.String(), nil
}

func emitRotateBlock(list []ast.Stmt, indent int, inFor bool) (string, error) {
	var b strings.Builder
	for i := 0; i < len(list); i++ {
		if i+2 < len(list) && isStatGroup(list[i], list[i+1], list[i+2]) {
			path := callArg(list[i].(*ast.AssignStmt).Rhs[0], 0)
			name, err := rotateExpr(path)
			if err != nil {
				return "", err
			}
			pad := strings.Repeat("    ", indent)
			b.WriteString(pad + "guard manager.fileExists(atPath: " + name + ") else { return }\n")
			b.WriteString(pad + "let info = try manager.attributesOfItem(atPath: " + name + ")\n")
			i += 2
			continue
		}
		line, err := emitRotateStmt(list[i], indent, inFor)
		if err != nil {
			return "", err
		}
		b.WriteString(line)
	}
	return b.String(), nil
}

func emitRotateStmt(stmt ast.Stmt, indent int, inFor bool) (string, error) {
	pad := strings.Repeat("    ", indent)
	switch s := stmt.(type) {
	case *ast.IfStmt:
		return emitRotateIf(s, indent, inFor)
	case *ast.ForStmt:
		return emitRotateFor(s, indent)
	case *ast.AssignStmt:
		line, err := emitRotateAssign(s)
		if err != nil {
			return "", err
		}
		return pad + line + "\n", nil
	case *ast.ReturnStmt:
		line, err := emitRotateReturn(s)
		if err != nil {
			return "", err
		}
		return pad + line + "\n", nil
	case *ast.BranchStmt:
		if s.Tok != token.CONTINUE || !inFor {
			return "", fmt.Errorf("rotate branch")
		}
		return pad + "i -= 1\n" + pad + "continue\n", nil
	default:
		return "", fmt.Errorf("rotate stmt %T", stmt)
	}
}

func emitRotateIf(s *ast.IfStmt, indent int, inFor bool) (string, error) {
	pad := strings.Repeat("    ", indent)
	if call, ok := callOf(s.Init, "os", "Rename"); ok && isErrCheck(s.Cond) && isReturnErr(s.Body) && s.Else == nil && len(call.Args) == 2 {
		from, err := rotateExpr(call.Args[0])
		if err != nil {
			return "", err
		}
		to, err := rotateExpr(call.Args[1])
		if err != nil {
			return "", err
		}
		return pad + "try manager.moveItem(atPath: " + from + ", toPath: " + to + ")\n", nil
	}
	if call, ok := callOf(s.Init, "os", "Remove"); ok && isRemoveCond(s.Cond) && isReturnErr(s.Body) && s.Else == nil {
		path, err := rotateExpr(call.Args[0])
		if err != nil {
			return "", err
		}
		return pad + "if manager.fileExists(atPath: " + path + ") {\n" +
			pad + "    try manager.removeItem(atPath: " + path + ")\n" +
			pad + "}\n", nil
	}
	if call, ok := callOf(s.Init, "os", "Stat"); ok && isNotExist(s.Cond) && isContinue(s.Body) && isErrReturn(s.Else) {
		path, err := rotateExpr(call.Args[0])
		if err != nil {
			return "", err
		}
		return pad + "if !manager.fileExists(atPath: " + path + ") {\n" +
			pad + "    i -= 1\n" +
			pad + "    continue\n" +
			pad + "}\n", nil
	}
	if s.Init != nil || s.Else != nil {
		return "", fmt.Errorf("rotate if")
	}
	if isDirCall(s.Cond) && len(s.Body.List) == 1 {
		text, args, ok := errorf(s.Body.List[0])
		if !ok || len(args) != 1 {
			return "", fmt.Errorf("rotate directory error")
		}
		arg, err := rotateExpr(args[0])
		if err != nil {
			return "", err
		}
		message, err := swiftFormat(text, []string{arg})
		if err != nil {
			return "", err
		}
		return pad + "if (info[.type] as? FileAttributeType) == .typeDirectory {\n" +
			pad + "    throw NSError(domain: \"VoiceRewriteLog\", code: 1, userInfo: [NSLocalizedDescriptionKey: " + message + "])\n" +
			pad + "}\n", nil
	}
	if sizeLess(s.Cond) && isReturnNil(s.Body) {
		return pad + "let size = (info[.size] as? NSNumber)?.int64Value ?? 0\n" +
			pad + "if size < maxBytes {\n" +
			pad + "    return\n" +
			pad + "}\n", nil
	}
	cond, err := rotateExpr(s.Cond)
	if err != nil {
		return "", err
	}
	if len(s.Body.List) != 1 {
		return "", fmt.Errorf("rotate if body")
	}
	text, args, ok := errorf(s.Body.List[0])
	if !ok || len(args) != 0 {
		return "", fmt.Errorf("rotate policy error")
	}
	return pad + "if " + cond + " {\n" +
		pad + "    throw NSError(domain: \"VoiceRewriteLog\", code: 1, userInfo: [NSLocalizedDescriptionKey: " + strconv.Quote(text) + "])\n" +
		pad + "}\n", nil
}

func emitRotateFor(s *ast.ForStmt, indent int) (string, error) {
	init, ok := s.Init.(*ast.AssignStmt)
	if !ok || s.Post == nil || s.Cond == nil || len(init.Lhs) != 1 || len(init.Rhs) != 1 || !isIdent(init.Lhs[0], "i") {
		return "", fmt.Errorf("rotate for")
	}
	dec, ok := s.Post.(*ast.IncDecStmt)
	if !ok || dec.Tok != token.DEC || !isIdent(dec.X, "i") {
		return "", fmt.Errorf("rotate for post")
	}
	start, err := rotateExpr(init.Rhs[0])
	if err != nil {
		return "", err
	}
	cond, err := rotateExpr(s.Cond)
	if err != nil {
		return "", err
	}
	body, err := emitRotateBlock(s.Body.List, indent+1, true)
	if err != nil {
		return "", err
	}
	pad := strings.Repeat("    ", indent)
	return pad + "var i = " + start + "\n" +
		pad + "while " + cond + " {\n" +
		body +
		pad + "    i -= 1\n" +
		pad + "}\n", nil
}

func emitRotateAssign(s *ast.AssignStmt) (string, error) {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return "", fmt.Errorf("rotate assign")
	}
	name, ok := s.Lhs[0].(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("rotate assign name")
	}
	if call, ok := callExpr(s.Rhs[0], "fmt", "Sprintf"); ok {
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return "", fmt.Errorf("rotate sprintf")
		}
		format, err := strconv.Unquote(lit.Value)
		if err != nil || format != "%s.%d" || len(call.Args) != 3 {
			return "", fmt.Errorf("rotate sprintf format")
		}
		base, err := rotateExpr(call.Args[1])
		if err != nil {
			return "", err
		}
		index, err := rotateExpr(call.Args[2])
		if err != nil {
			return "", err
		}
		return "let " + name.Name + " = \"\\(" + base + ").\\(" + index + ")\"", nil
	}
	right, err := rotateExpr(s.Rhs[0])
	if err != nil {
		return "", err
	}
	return "let " + name.Name + " = " + right, nil
}

func emitRotateReturn(s *ast.ReturnStmt) (string, error) {
	if len(s.Results) != 1 {
		return "", fmt.Errorf("rotate return")
	}
	if call, ok := callExpr(s.Results[0], "os", "Rename"); ok && len(call.Args) == 2 {
		from, err := rotateExpr(call.Args[0])
		if err != nil {
			return "", err
		}
		to, err := rotateExpr(call.Args[1])
		if err != nil {
			return "", err
		}
		return "try manager.moveItem(atPath: " + from + ", toPath: " + to + ")", nil
	}
	if isIdent(s.Results[0], "nil") {
		return "return", nil
	}
	return "", fmt.Errorf("rotate return")
}

func rotateExpr(e ast.Expr) (string, error) {
	switch n := e.(type) {
	case *ast.Ident:
		if n.Name == "nil" {
			return "nil", nil
		}
		return n.Name, nil
	case *ast.BasicLit:
		if n.Kind == token.INT {
			return n.Value, nil
		}
		if n.Kind == token.STRING {
			text, err := strconv.Unquote(n.Value)
			if err != nil {
				return "", err
			}
			return strconv.Quote(text), nil
		}
		return "", fmt.Errorf("rotate lit")
	case *ast.BinaryExpr:
		left, err := rotateExpr(n.X)
		if err != nil {
			return "", err
		}
		right, err := rotateExpr(n.Y)
		if err != nil {
			return "", err
		}
		return left + " " + n.Op.String() + " " + right, nil
	case *ast.ParenExpr:
		inner, err := rotateExpr(n.X)
		if err != nil {
			return "", err
		}
		return "(" + inner + ")", nil
	case *ast.UnaryExpr:
		inner, err := rotateExpr(n.X)
		if err != nil {
			return "", err
		}
		return n.Op.String() + inner, nil
	default:
		return "", fmt.Errorf("rotate expr %T", e)
	}
}

func swiftFormat(format string, args []string) (string, error) {
	if strings.Count(format, "%s") != len(args) || strings.Contains(format, "%") && strings.Count(format, "%s") != strings.Count(format, "%") {
		return "", fmt.Errorf("rotate format %q", format)
	}
	var b strings.Builder
	b.WriteByte('"')
	parts := strings.Split(format, "%s")
	for i, part := range parts {
		b.WriteString(part)
		if i < len(args) {
			b.WriteString(`\(` + args[i] + `)`)
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}

func isStatGroup(a, b, c ast.Stmt) bool {
	as, ok := a.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 2 || len(as.Rhs) != 1 || !isIdent(as.Lhs[0], "info") || !isIdent(as.Lhs[1], "err") {
		return false
	}
	if _, ok := callExpr(as.Rhs[0], "os", "Stat"); !ok {
		return false
	}
	missing, ok := b.(*ast.IfStmt)
	if !ok || missing.Init != nil || missing.Else != nil || !isNotExist(missing.Cond) || !isReturnNil(missing.Body) {
		return false
	}
	failed, ok := c.(*ast.IfStmt)
	if !ok || failed.Init != nil || failed.Else != nil || !isErrCheck(failed.Cond) || !isReturnErr(failed.Body) {
		return false
	}
	return true
}

func isNotExist(e ast.Expr) bool {
	call, ok := callExpr(e, "os", "IsNotExist")
	return ok && len(call.Args) == 1 && isIdent(call.Args[0], "err")
}

func isErrCheck(e ast.Expr) bool {
	bin, ok := e.(*ast.BinaryExpr)
	return ok && bin.Op == token.NEQ && isIdent(bin.X, "err") && isIdent(bin.Y, "nil")
}

func isRemoveCond(e ast.Expr) bool {
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.LAND || !isErrCheck(bin.X) {
		return false
	}
	unary, ok := bin.Y.(*ast.UnaryExpr)
	return ok && unary.Op == token.NOT && isNotExist(unary.X)
}

func isDirCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && isIdent(sel.X, "info") && sel.Sel.Name == "IsDir" && len(call.Args) == 0
}

func sizeLess(e ast.Expr) bool {
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.LSS || !isIdent(bin.Y, "maxBytes") {
		return false
	}
	call, ok := bin.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && isIdent(sel.X, "info") && sel.Sel.Name == "Size" && len(call.Args) == 0
}

func isReturnNil(body *ast.BlockStmt) bool {
	return body != nil && len(body.List) == 1 && len(returnResults(body.List[0])) == 1 && isIdent(returnResults(body.List[0])[0], "nil")
}

func isReturnErr(body *ast.BlockStmt) bool {
	results := returnResults(onlyStmt(body))
	return len(results) == 1 && isIdent(results[0], "err")
}

func isContinue(body *ast.BlockStmt) bool {
	branch, ok := onlyStmt(body).(*ast.BranchStmt)
	return ok && branch.Tok == token.CONTINUE
}

func isErrReturn(stmt ast.Stmt) bool {
	next, ok := stmt.(*ast.IfStmt)
	return ok && next.Else == nil && isErrCheck(next.Cond) && isReturnErr(next.Body)
}

func onlyStmt(body *ast.BlockStmt) ast.Stmt {
	if body == nil || len(body.List) != 1 {
		return nil
	}
	return body.List[0]
}

func returnResults(stmt ast.Stmt) []ast.Expr {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok {
		return nil
	}
	return ret.Results
}

func errorf(stmt ast.Stmt) (string, []ast.Expr, bool) {
	results := returnResults(stmt)
	if len(results) != 1 {
		return "", nil, false
	}
	call, ok := callExpr(results[0], "fmt", "Errorf")
	if !ok || len(call.Args) == 0 {
		return "", nil, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", nil, false
	}
	text, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", nil, false
	}
	return text, call.Args[1:], true
}

func callOf(stmt ast.Stmt, pkg, name string) (*ast.CallExpr, bool) {
	as, ok := stmt.(*ast.AssignStmt)
	if !ok || len(as.Rhs) != 1 {
		return nil, false
	}
	return callExpr(as.Rhs[0], pkg, name)
}

func callArg(e ast.Expr, index int) ast.Expr {
	call, _ := e.(*ast.CallExpr)
	return call.Args[index]
}

func callExpr(e ast.Expr, pkg, name string) (*ast.CallExpr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name || !isIdent(sel.X, pkg) {
		return nil, false
	}
	return call, true
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id != nil && id.Name == name
}

func replaceSwiftFunc(source, name, replacement string) (string, error) {
	marker := "static func " + name + "("
	start := strings.Index(source, marker)
	if start < 0 {
		return "", fmt.Errorf("missing %s", name)
	}
	line := strings.LastIndex(source[:start], "\n") + 1
	prefix := source[:line]
	const note = "    // Source: voice-rewrite/internal/core/logfile/rotate.go\n"
	prefix = strings.TrimSuffix(prefix, note)
	rel := start - line
	brace := strings.Index(source[start:], "{")
	if brace < 0 {
		return "", fmt.Errorf("missing rotate brace")
	}
	depth := 0
	end := start + brace
	for ; end < len(source); end++ {
		switch source[end] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end++
				goto done
			}
		}
	}
	return "", fmt.Errorf("unbalanced rotate")
done:
	if rel < 0 {
		return "", fmt.Errorf("rotate position")
	}
	return prefix + replacement + source[end:], nil
}
