// Package auth — пароли (argon2id) и подписанные токены сессий.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func CheckPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var (
	ErrBadToken = errors.New("auth: неверный токен")
	ErrExpired  = errors.New("auth: срок токена истёк")
)

// Signer выпускает токены сессий: id пользователя, срок, подпись HMAC.
// Сессии не хранятся в базе; отзыв — смена секрета или блокировка
// пользователя, которая проверяется при каждом запросе.
type Signer struct {
	key []byte
}

func NewSigner(key []byte) *Signer { return &Signer{key: key} }

func (s *Signer) Issue(userID int64, ttl time.Duration) string {
	payload := make([]byte, 16)
	binary.BigEndian.PutUint64(payload[0:], uint64(userID))
	binary.BigEndian.PutUint64(payload[8:], uint64(time.Now().Add(ttl).Unix()))
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Signer) Verify(tok string) (int64, error) {
	p, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return 0, ErrBadToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil || len(payload) != 16 {
		return 0, ErrBadToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return 0, ErrBadToken
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return 0, ErrBadToken
	}
	if time.Now().Unix() > int64(binary.BigEndian.Uint64(payload[8:])) {
		return 0, ErrExpired
	}
	return int64(binary.BigEndian.Uint64(payload[0:])), nil
}

// NewAPIToken — токен для автоматизации. В базе хранится только хеш.
func NewAPIToken() (plain string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	plain = "rn_" + base64.RawURLEncoding.EncodeToString(b)
	return plain, HashAPIToken(plain), nil
}

func HashAPIToken(plain string) []byte {
	h := sha256.Sum256([]byte(plain))
	return h[:]
}
