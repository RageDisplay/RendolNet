package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"rendolnet/internal/platform/auth"
	"rendolnet/internal/store"
)

// EnsureAdmin создаёт администратора admin, если пользователей нет совсем.
func EnsureAdmin(ctx context.Context, log *slog.Logger, st *store.Store, password string) error {
	n, err := st.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	h, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	log.Warn("создан пользователь admin с паролем из RN_ADMIN_PASSWORD (по умолчанию admin)")
	return st.CreateUser(ctx, &store.User{Username: "admin", FullName: "Администратор", Role: store.RoleAdmin, PasswordHash: h})
}

// SessionKey берёт ключ подписи сессий из настроек или создаёт новый.
func SessionKey(ctx context.Context, st *store.Store) ([]byte, error) {
	var key []byte
	if err := st.Setting(ctx, "session_key", &key); err == nil && len(key) >= 32 {
		return key, nil
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, st.PutSetting(ctx, "session_key", key)
}

// WatchAdminReset следит за файлом, который кладёт консоль узла по пункту
// «сбросить пароль администратора». Физический доступ к консоли считается
// достаточным правом.
func (s *Server) WatchAdminReset(ctx context.Context) {
	flag := filepath.Join(s.cfg.HostDir, "reset-admin")
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if _, err := os.Stat(flag); err != nil {
			continue
		}
		_ = os.Remove(flag)
		h, _ := auth.HashPassword("admin")
		u, err := s.st.UserByName(ctx, "admin")
		if errors.Is(err, store.ErrNotFound) {
			err = s.st.CreateUser(ctx, &store.User{Username: "admin", Role: store.RoleAdmin, PasswordHash: h})
		} else if err == nil {
			u.Disabled = false
			u.Role = store.RoleAdmin
			if err = s.st.UpdateUser(ctx, u); err == nil {
				err = s.st.SetPassword(ctx, u.ID, h)
			}
		}
		if err != nil {
			s.log.Error("сброс пароля admin", "err", err)
			continue
		}
		s.st.Audit(ctx, store.AuditEntry{Username: "console", Action: "user.reset_admin", Target: "admin"})
		s.log.Warn("пароль admin сброшен с консоли узла")
	}
}

// LoadOrCreateCert берёт сертификат из dir или выпускает самоподписанный.
// Свой сертификат администратор кладёт в те же файлы.
func LoadOrCreateCert(dir string) (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return c, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	host, _ := os.Hostname()
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"RendolNet"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host, "rendolnet", "localhost"},
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLinkLocalUnicast() {
				tmpl.IPAddresses = append(tmpl.IPAddresses, n.IP)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return tls.Certificate{}, err
	}
	c, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("api: только что выпущенный сертификат не читается: %w", err)
	}
	return c, nil
}
