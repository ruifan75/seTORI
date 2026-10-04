package youtube

import "net/http"

// SetTransport は HTTP の往復を差し替える（テスト用）。
//
// **本番の経路をサービス層のテストから通すために要る**（issue #56）。
// 「YouTube が取れないと明言した」を取り直しの間隔へ伝えるのは複数の層を跨ぐので、
// 判定関数だけを検査しても、呼び出し側がその結果を捨てていれば気付けない。
//
// 本番コードからは呼ばない。使う前（要求を出す前）に呼ぶこと。
func (c *Client) SetTransport(rt http.RoundTripper) {
	c.httpClient = &http.Client{Transport: rt}
}
