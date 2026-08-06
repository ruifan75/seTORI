package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("s3cret-pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "s3cret-pw" {
		t.Fatal("hash must not equal plaintext")
	}

	ok, err := VerifyPassword("s3cret-pw", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("correct password should verify")
	}

	ok, err = VerifyPassword("wrong-pw", hash)
	if err != nil {
		t.Fatalf("VerifyPassword(wrong): %v", err)
	}
	if ok {
		t.Fatal("wrong password must not verify")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	h1, _ := HashPassword("same")
	h2, _ := HashPassword("same")
	if h1 == h2 {
		t.Fatal("hashes of the same password must differ (random salt)")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-valid-hash"); err == nil {
		t.Fatal("expected error for malformed hash")
	}
}

func TestGenerateTokenUnique(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	b, _ := GenerateToken()
	if a == "" || a == b {
		t.Fatalf("tokens must be non-empty and unique: %q %q", a, b)
	}
	if HashToken(a) == a {
		t.Fatal("HashToken must not return the raw token")
	}
}

func TestHasPermission(t *testing.T) {
	cases := []struct {
		perms []string
		need  string
		want  bool
	}{
		{[]string{"content:edit"}, "content:edit", true},
		{[]string{"content:edit"}, "users:manage", false},
		{[]string{"*"}, "users:manage", true}, // ワイルドカードは全許可
		{[]string{}, "content:edit", false},   // 権限なし
		{[]string{"content:edit"}, "", true},  // 特定権限不要
	}
	for _, c := range cases {
		if got := HasPermission(c.perms, c.need); got != c.want {
			t.Errorf("HasPermission(%v, %q) = %v, want %v", c.perms, c.need, got, c.want)
		}
	}
}
