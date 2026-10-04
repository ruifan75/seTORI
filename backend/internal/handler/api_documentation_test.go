package handler

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type documentedAPIRoute struct {
	permission string
	login      bool
	line       int
}

// 期待値は人が書いた docs/API.md。そのまま読んで使い、登録や認可表から作らない。
// AST は登録の棚卸しだけに使い、実際の ServeMux と認可関数も呼ぶ。
func TestAPIDocumentation(t *testing.T) {
	data, err := os.ReadFile("../../../docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	documented, err := parseAPIDocumentation(string(data))
	if err != nil {
		t.Fatal(err)
	}
	registered := registeredAPIPatterns(t)
	for _, pattern := range sortedAPIPatterns(registered) {
		if _, ok := documented[pattern]; !ok {
			t.Errorf("registered route missing from docs/API.md: %s", pattern)
		}
	}
	for _, pattern := range sortedAPIPatterns(documented) {
		if _, ok := registered[pattern]; !ok {
			t.Errorf("docs/API.md:%d: documented route not registered: %s", documented[pattern].line, pattern)
		}
	}
	if t.Failed() {
		return
	}

	r := &Router{mux: http.NewServeMux()}
	r.setupRoutes()
	for _, pattern := range sortedAPIPatterns(documented) {
		want := documented[pattern]
		t.Run(pattern, func(t *testing.T) {
			parts := strings.SplitN(pattern, " ", 2)
			segments := strings.Split(parts[1], "/")
			for i, segment := range segments {
				if strings.HasPrefix(segment, "{") {
					segments[i] = "api-doc-id"
				}
			}
			path := strings.Join(segments, "/")
			methods := []string{parts[0]}
			if parts[0] == http.MethodGet {
				methods = append(methods, http.MethodHead)
			}
			for _, method := range methods {
				req := httptest.NewRequest(method, path, nil)
				if _, actual := r.mux.Handler(req); actual != pattern {
					t.Errorf("%s %s selects %q, document says %q", method, path, actual, pattern)
				}
				permission, login := requiredPermission(method, authzPath(req.URL.EscapedPath()))
				if permission != want.permission || login != want.login {
					t.Errorf("docs/API.md:%d: authorization=(%q,%t), want (%q,%t)", want.line, permission, login, want.permission, want.login)
				}
			}
		})
	}
}

func sortedAPIPatterns[V any](routes map[string]V) []string {
	patterns := make([]string, 0, len(routes))
	for pattern := range routes {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns
}

// 全 handler ファイルを見る。setupRoutes の外の Handle/HandleFunc も取りこぼさない。
// 動的な登録は一覧を確定できないので、黙って無視せず検査を失敗させる。
func registeredAPIPatterns(t *testing.T) map[string]bool {
	t.Helper()
	packages, err := parser.ParseDir(token.NewFileSet(), ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	routes := make(map[string]bool)
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
					return true
				}
				if len(call.Args) != 2 {
					t.Error("API route registration must have 2 arguments")
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Error("dynamic API route registration requires an explicit documentation inventory")
					return true
				}
				pattern, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Error(err)
					return true
				}
				if routes[pattern] {
					t.Errorf("duplicate API route registration: %s", pattern)
				}
				routes[pattern] = true
				return true
			})
		}
	}
	return routes
}

// 表のメソッド・パス・認可を読む。説明の表や本文中の API の例は期待値にしない。
func parseAPIDocumentation(markdown string) (map[string]documentedAPIRoute, error) {
	routes := make(map[string]documentedAPIRoute)
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		pathCell := strings.TrimSpace(cells[2])
		if !strings.HasPrefix(strings.Trim(pathCell, "`"), "/") {
			continue
		}
		if len(cells) != 8 || cells[0] != "" || cells[7] != "" {
			return nil, fmt.Errorf("API.md:%d: route table must have 6 columns", lineNumber)
		}
		for i := 1; i <= 6; i++ {
			cells[i] = strings.TrimSpace(cells[i])
			if cells[i] == "" {
				return nil, fmt.Errorf("API.md:%d: empty route column %d", lineNumber, i)
			}
		}
		method, err := apiDocLiteral(cells[1])
		if err != nil {
			return nil, fmt.Errorf("API.md:%d: method: %w", lineNumber, err)
		}
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		default:
			return nil, fmt.Errorf("API.md:%d: invalid HTTP method %q", lineNumber, method)
		}
		path, err := apiDocLiteral(cells[2])
		if err != nil {
			return nil, fmt.Errorf("API.md:%d: path: %w", lineNumber, err)
		}
		if strings.ContainsAny(path, " \t?#") {
			return nil, fmt.Errorf("API.md:%d: invalid route path %q", lineNumber, path)
		}
		pattern := method + " " + path
		if _, exists := routes[pattern]; exists {
			return nil, fmt.Errorf("API.md:%d: duplicate documented route %s", lineNumber, pattern)
		}
		route := documentedAPIRoute{line: lineNumber}
		switch cells[3] {
		case "未ログイン可":
		case "ログインのみ":
			route.login = true
		default:
			route.permission, err = apiDocLiteral(cells[3])
			if err != nil {
				return nil, fmt.Errorf("API.md:%d: authorization: %w", lineNumber, err)
			}
			route.login = true
		}
		routes[pattern] = route
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("API.md: no documented routes")
	}
	return routes, nil
}

func apiDocLiteral(cell string) (string, error) {
	if len(cell) < 3 || cell[0] != '`' || cell[len(cell)-1] != '`' || strings.Contains(cell[1:len(cell)-1], "`") {
		return "", fmt.Errorf("expected one nonempty backtick literal, got %q", cell)
	}
	return cell[1 : len(cell)-1], nil
}

func TestParseAPIDocumentation(t *testing.T) {
	row := "| `GET` | `/api/example` | 未ログイン可 | 一覧する | — | `{items}` |\n"
	otherMethod := strings.Replace(row, "`GET`", "`POST`", 1)
	for _, tc := range []struct {
		name, markdown string
		wantError      bool
	}{
		{"route", row, false},
		{"different methods", row + otherMethod, false},
		{"prose is not inventory", "本文の `GET /api/example`", true},
		{"duplicate", row + row, true},
		{"missing input", strings.Replace(row, "| — |", "|  |", 1), true},
		{"missing response", strings.Replace(row, "| `{items}` |", "|  |", 1), true},
		{"wrong column count", strings.Replace(row, "| — |", "|", 1), true},
		{"lowercase method", strings.Replace(row, "`GET`", "`get`", 1), true},
		{"unquoted method", strings.Replace(row, "`GET`", "GET", 1), true},
		{"unquoted path", strings.Replace(row, "`/api/example`", "/api/example", 1), true},
		{"unknown authorization format", strings.Replace(row, "未ログイン可", "管理者のみ", 1), true},
		{"query in path", strings.Replace(row, "/api/example", "/api/example?q=x", 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAPIDocumentation(tc.markdown)
			if (err != nil) != tc.wantError {
				t.Fatalf("parse error=%v, wantError=%t", err, tc.wantError)
			}
		})
	}
}
