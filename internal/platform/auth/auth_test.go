package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("admin")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "admin") {
		t.Fatal("верный пароль не принят")
	}
	if CheckPassword(h, "Admin") {
		t.Fatal("неверный пароль принят")
	}
	if CheckPassword("garbage", "admin") {
		t.Fatal("испорченный хеш принят")
	}
}

func TestSigner(t *testing.T) {
	s := NewSigner([]byte("k1"))
	tok := s.Issue(42, time.Minute)
	if id, err := s.Verify(tok); err != nil || id != 42 {
		t.Fatalf("Verify: %d %v", id, err)
	}
	if _, err := NewSigner([]byte("k2")).Verify(tok); !errors.Is(err, ErrBadToken) {
		t.Fatalf("чужая подпись: %v", err)
	}
	if _, err := s.Verify(s.Issue(1, -time.Second)); !errors.Is(err, ErrExpired) {
		t.Fatalf("просроченный: %v", err)
	}
	broken := strings.Replace(tok, ".", ".x", 1)
	if _, err := s.Verify(broken); err == nil {
		t.Fatal("испорченный токен принят")
	}
}
