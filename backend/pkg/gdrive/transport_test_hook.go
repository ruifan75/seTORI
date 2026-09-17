package gdrive

import "net/http"

// SetTransport は HTTP の往復を差し替える（テスト用）。
//
// **本番の経路をテストから呼べるようにするために要る。** 純粋関数だけを検査すると、
// 呼び出し側でその関数を使うのをやめても通ってしまう ── 実際 issue #64 の
// レビューで「アップロード名から印を外す」「整理で全件を消す」改変が
// どちらもテストを素通りした。
//
// 本番コードからは呼ばない。
func (c *Client) SetTransport(rt http.RoundTripper) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.http = &http.Client{Transport: rt}
}
