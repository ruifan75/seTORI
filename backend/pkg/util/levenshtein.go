package util

import (
	"golang.org/x/text/unicode/norm"
)

// NormalizeUnicode NFKC で Unicode 文字列を正規化
// 例：全角ローマ数字 Ⅱ (U+2161) → II, 全角英数 Ａ → A
func NormalizeUnicode(s string) string {
	return norm.NFKC.String(s)
}

// LevenshteinDistance 2つの文字列の編集距離を計算
func LevenshteinDistance(s1, s2 string) int {
	r1 := []rune(s1)
	r2 := []rune(s2)

	len1 := len(r1)
	len2 := len(r2)

	if len1 == 0 {
		return len2
	}
	if len2 == 0 {
		return len1
	}

	// 建立 2D 矩陣
	matrix := make([][]int, len1+1)
	for i := range matrix {
		matrix[i] = make([]int, len2+1)
	}

	// 初始化第一列和第一行
	for i := 0; i <= len1; i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = j
	}

	// 填充矩陣
	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,      // 刪除
				matrix[i][j-1]+1,      // 插入
				matrix[i-1][j-1]+cost, // 替換
			)
		}
	}

	return matrix[len1][len2]
}

// Similarity 計算兩個字串的相似度 (0.0 - 1.0)
// 比較前先做 NFKC 正規化（如 Ⅱ → II）
func Similarity(s1, s2 string) float64 {
	s1 = NormalizeUnicode(s1)
	s2 = NormalizeUnicode(s2)

	if s1 == s2 {
		return 1.0
	}

	r1 := []rune(s1)
	r2 := []rune(s2)
	maxLen := len(r1)
	if len(r2) > maxLen {
		maxLen = len(r2)
	}

	if maxLen == 0 {
		return 1.0
	}

	distance := LevenshteinDistance(s1, s2)
	return 1.0 - float64(distance)/float64(maxLen)
}

// min 返回三個數的最小值
func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
