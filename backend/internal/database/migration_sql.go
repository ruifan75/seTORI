package database

import (
	"fmt"
	"strings"
)

// runner が Tx を所有する。SQL 内で終えてから BEGIN し直すと、sql.Tx の
// Commit が成功しても DDL と適用記録は別々に確定するため、実行前に拒否する。
// SQL 全体の構文検査ではない。CONCURRENTLY / VACUUM 等の Tx 非対応文は
// PostgreSQL が拒否し、applyMigration がファイル名付きで返してロールバックする。
func validateMigrationSQL(query string) error {
	var head []string
	statement := 1
	atomicDepth := 0
	previous := ""
	check := func() error {
		if len(head) == 0 {
			return nil
		}
		command := head[0]
		switch command {
		case "BEGIN", "COMMIT", "END", "ROLLBACK", "ABORT", "SAVEPOINT", "RELEASE":
		case "START", "PREPARE":
			if len(head) < 2 || head[1] != "TRANSACTION" {
				return nil
			}
			command += " TRANSACTION"
		default:
			return nil
		}
		return fmt.Errorf("文 %d: %s は使えません（トランザクションは migration runner が管理します）", statement, command)
	}
	for i := 0; i < len(query); {
		token, next, err := migrationSQLToken(query, i)
		if err != nil {
			return err
		}
		i = next
		if token == "" { // 空白・コメント
			continue
		}
		// SQL 関数の BEGIN ATOMIC ... END 本体のセミコロンは文の区切りではない。
		// PL/pgSQL の dollar quote 本体は lexer が1トークンとして扱う。
		if token == "ATOMIC" && previous == "BEGIN" {
			atomicDepth++
		} else if atomicDepth > 0 {
			if token == "CASE" {
				atomicDepth++
			} else if token == "END" {
				atomicDepth--
			}
		}
		previous = token
		if token == ";" && atomicDepth == 0 {
			if err := check(); err != nil {
				return err
			}
			if len(head) > 0 {
				statement++
			}
			head = nil
		} else if len(head) < 2 {
			head = append(head, token)
		}
	}
	return check()
}

// 文頭のキーワードだけを読むための lexer。引用値は ? にして、値の中の
// COMMIT や ; を文として扱わない。入れ子のコメントと E'...' も区別する。
func migrationSQLToken(query string, i int) (string, int, error) {
	start := i
	c := query[i]
	if strings.ContainsRune(" \t\r\n\f\v", rune(c)) {
		return "", i + 1, nil
	}
	if strings.HasPrefix(query[i:], "--") {
		if end := strings.IndexAny(query[i:], "\r\n"); end >= 0 {
			return "", i + end + 1, nil
		}
		return "", len(query), nil
	}
	if strings.HasPrefix(query[i:], "/*") {
		depth := 1
		for i += 2; i < len(query); {
			switch {
			case strings.HasPrefix(query[i:], "/*"):
				depth++
				i += 2
			case strings.HasPrefix(query[i:], "*/"):
				depth--
				i += 2
				if depth == 0 {
					return "", i, nil
				}
			default:
				i++
			}
		}
		return "", i, fmt.Errorf("byte %d: 閉じていない SQL コメント", start)
	}
	escaped := (c == 'E' || c == 'e') && i+1 < len(query) && query[i+1] == '\''
	if escaped {
		i++
		c = query[i]
	}
	if c == '\'' || c == '"' {
		for i++; i < len(query); i++ {
			if escaped && query[i] == '\\' {
				i++
			} else if query[i] == c {
				if i+1 < len(query) && query[i+1] == c {
					i++
				} else {
					return "?", i + 1, nil
				}
			}
		}
		return "", i, fmt.Errorf("byte %d: 閉じていない SQL 引用", start)
	}
	if c == '$' {
		end := i + 1
		if end < len(query) && migrationSQLIdentStart(query[end]) {
			for end++; end < len(query) && migrationSQLIdentPart(query[end]) && query[end] != '$'; end++ {
			}
		}
		if end < len(query) && query[end] == '$' {
			delimiter := query[i : end+1]
			if close := strings.Index(query[end+1:], delimiter); close >= 0 {
				return "?", end + 1 + close + len(delimiter), nil
			}
			return "", len(query), fmt.Errorf("byte %d: 閉じていない SQL dollar quote", start)
		}
	}
	if migrationSQLIdentStart(c) {
		for i++; i < len(query) && migrationSQLIdentPart(query[i]); i++ {
		}
		return strings.ToUpper(query[start:i]), i, nil
	}
	return string(c), i + 1, nil
}

func migrationSQLIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128
}

func migrationSQLIdentPart(c byte) bool {
	return migrationSQLIdentStart(c) || c >= '0' && c <= '9' || c == '$'
}
