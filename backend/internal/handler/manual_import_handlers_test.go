package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func uploadRequest(t *testing.T, contentType string, content io.Reader) *http.Request {
	t.Helper()
	body := io.MultiReader(
		strings.NewReader("--test-boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"test.json\"\r\nContent-Type: application/json\r\n\r\n"),
		content,
		strings.NewReader("\r\n--test-boundary--\r\n"),
	)
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", contentType)
	t.Cleanup(func() {
		req.Body.Close()
		if req.MultipartForm != nil {
			req.MultipartForm.RemoveAll()
		}
	})
	return req
}

// 有効な multipart body でも、間違った header は受け取った値を示して拒否する。
func TestOpenUploadContentType(t *testing.T) {
	for _, tt := range []struct {
		name, contentType, want string
	}{
		{"json", "application/json", `multipart/form-data で送ってください（受け取った Content-Type: "application/json"）`},
		{"missing", "", `multipart/form-data で送ってください（受け取った Content-Type: ""）`},
		{"similar_type", "multipart/form-data-extra; boundary=test-boundary", `multipart/form-data で送ってください（受け取った Content-Type: "multipart/form-data-extra; boundary=test-boundary"）`},
		{"contains_type", "x-multipart/form-data; boundary=test-boundary", `multipart/form-data で送ってください（受け取った Content-Type: "x-multipart/form-data; boundary=test-boundary"）`},
		{"type_in_parameter", `application/json; note="multipart/form-data"; boundary=test-boundary`, `multipart/form-data で送ってください（受け取った Content-Type: "application/json; note=\"multipart/form-data\"; boundary=test-boundary"）`},
		{"malformed", `multipart/form-data; boundary="unterminated`, `multipart/form-data で送ってください（受け取った Content-Type: "multipart/form-data; boundary=\"unterminated"）`},
		{"missing_boundary", "multipart/form-data", `multipart/form-data の boundary が指定されていません（受け取った Content-Type: "multipart/form-data"）`},
		{"empty_boundary", `multipart/form-data; boundary=""`, `multipart/form-data の boundary が指定されていません（受け取った Content-Type: "multipart/form-data; boundary=\"\""）`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := uploadRequest(t, tt.contentType, strings.NewReader("upload contents"))
			f, err := openUpload(httptest.NewRecorder(), req, maxLiveChatUploadBytes)
			if f != nil {
				f.Close()
				t.Fatal("invalid Content-Type accepted")
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestOpenUploadValidMultipart(t *testing.T) {
	for _, ct := range []string{
		"multipart/form-data; boundary=test-boundary",
		`Multipart/Form-Data; boundary="test-boundary"`,
	} {
		t.Run(ct, func(t *testing.T) {
			req := uploadRequest(t, ct, strings.NewReader("upload contents"))
			f, err := openUpload(httptest.NewRecorder(), req, maxLiveChatUploadBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil || string(data) != "upload contents" {
				t.Fatalf("content = %q, error = %v", data, err)
			}
		})
	}
}

// 上限ぶんの file に multipart の外殻が加わると、実際の body 上限を超える。
// body を流して作り、64MB の入力をテスト自身のメモリへ載せない。
func TestOpenUploadSizeLimit(t *testing.T) {
	for _, tt := range []struct {
		name  string
		limit int64
		want  string
	}{
		{"live_chat", maxLiveChatUploadBytes, "ファイルを読み取れません（上限 64MB を超えていないか確認してください）"},
		{"info_json", maxInfoJSONUploadBytes, "ファイルを読み取れません（上限 32MB を超えていないか確認してください）"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := uploadRequest(t, "multipart/form-data; boundary=test-boundary", io.LimitReader(uploadZeroReader{}, tt.limit))
			f, err := openUpload(httptest.NewRecorder(), req, tt.limit)
			if f != nil {
				f.Close()
				t.Fatal("oversized body accepted")
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

type uploadZeroReader struct{}

func (uploadZeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// 上限に当たっていない解析失敗を、上限の文言で報告しない（issue #50）。
//
// 原因の部分は Go の mime パッケージの文言なので、版が変わっても落ちないよう
// こちらで書いた前置きと「上限の文言でない」ことだけを見る。
func TestOpenUploadMalformedMultipart(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("--test-boundary\r\nbroken header\r\n"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test-boundary")
	_, err := openUpload(httptest.NewRecorder(), req, maxLiveChatUploadBytes)
	if err == nil {
		t.Fatal("壊れた multipart を受理している")
	}
	if !strings.HasPrefix(err.Error(), "multipart/form-data のファイルを読み取れません: ") {
		t.Errorf("error = %q, want 解析失敗の文言", err)
	}
	if strings.Contains(err.Error(), "上限") {
		t.Errorf("上限に当たっていないのに上限の文言になっている: %q", err)
	}
}
