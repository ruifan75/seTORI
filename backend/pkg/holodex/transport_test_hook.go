package holodex

import "net/http"

// SetTransport は HTTP の往復を差し替える（テスト用。youtube.Client.SetTransport と同じ理由）。
//
// 本番コードからは呼ばない。使う前（要求を出す前）に呼ぶこと。
func (c *Client) SetTransport(rt http.RoundTripper) {
	c.httpClient = &http.Client{Transport: rt}
}
