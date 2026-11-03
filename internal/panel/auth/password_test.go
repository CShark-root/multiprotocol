package auth

import "testing"

func TestHashAndVerify(t *testing.T) {
	h := HashPassword("correct horse battery staple")
	if h == HashPassword("correct horse battery staple") {
		t.Fatal("hashes must be salted")
	}
	ok, err := VerifyPassword("correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatalf("valid password rejected: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong", h)
	if err != nil || ok {
		t.Fatalf("wrong password accepted: ok=%v err=%v", ok, err)
	}
	if _, err := VerifyPassword("x", "$bcrypt$whatever"); err == nil {
		t.Fatal("malformed hash must be an error")
	}
}
