package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rendolnet/internal/update"
	"rendolnet/internal/zapret"
)

type Settings struct {
	EventRetentionDays   int    `json:"event_retention_days"`
	TrafficRetentionDays int    `json:"traffic_retention_days"`
	LoginBanner          string `json:"login_banner"`
	DefaultConfirmSec    int    `json:"default_confirm_seconds"`
}

func defaultSettings() Settings {
	return Settings{EventRetentionDays: 14, TrafficRetentionDays: 30, DefaultConfirmSec: 120}
}

func (s *Server) settings(ctx context.Context) Settings {
	set := defaultSettings()
	_ = s.st.Setting(ctx, "ui", &set)
	if set.EventRetentionDays <= 0 {
		set.EventRetentionDays = 14
	}
	if set.TrafficRetentionDays <= 0 {
		set.TrafficRetentionDays = 30
	}
	return set
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settings(r.Context()))
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var set Settings
	if !readJSON(w, r, &set) {
		return
	}
	if set.EventRetentionDays < 1 || set.EventRetentionDays > 365 || set.TrafficRetentionDays < 1 || set.TrafficRetentionDays > 365 {
		writeErr(w, http.StatusBadRequest, "срок хранения — от 1 до 365 дней")
		return
	}
	if set.DefaultConfirmSec < 0 || set.DefaultConfirmSec > 3600 {
		writeErr(w, http.StatusBadRequest, "время подтверждения — от 0 до 3600 секунд")
		return
	}
	if err := s.st.PutSetting(r.Context(), "ui", set); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "settings.update", "", set)
	writeJSON(w, http.StatusOK, set)
}

// Баннер нужен на странице входа, до авторизации.
func (s *Server) handleBanner(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"banner": s.settings(r.Context()).LoginBanner})
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"version":     s.cfg.Version,
		"api_started": s.started,
		"host":        s.host.snapshot(s.cfg.DataDir),
		"update":      update.ReadStatus(s.cfg.HostDir),
	}
	if b, err := os.ReadFile(filepath.Join(s.cfg.HostDir, update.VersionFile)); err == nil {
		resp["installed_version"] = string(b)
	}
	if st, err := s.agent.Status(r.Context()); err == nil {
		resp["agent"] = st
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	log, err := s.st.AuditLog(r.Context(), limit, before)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, log)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) hostRequest(r *http.Request, act update.Action, version string) (*update.Request, error) {
	st := update.ReadStatus(s.cfg.HostDir)
	if st.State == update.StateQueued || st.State == update.StateRunning {
		return nil, errors.New("узел уже выполняет операцию: " + string(st.Action))
	}
	req := &update.Request{ID: newID(), Action: act, Version: version, User: userFrom(r).Username, Created: time.Now()}
	if err := update.WriteJSON(filepath.Join(s.cfg.HostDir, update.StatusFile), update.Status{
		RequestID: req.ID, Action: act, State: update.StateQueued, Version: version,
	}); err != nil {
		return nil, err
	}
	return req, update.WriteJSON(filepath.Join(s.cfg.HostDir, update.RequestFile), req)
}

var sshKeyRe = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)) [A-Za-z0-9+/]{40,}={0,3}( [ -~]{0,100})?$`)

// handleSSH включает вход по SSH под root по ключу или выключает его.
// Пустой ключ — выключить.
func (s *Server) handleSSH(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key != "" && !sshKeyRe.MatchString(req.Key) {
		writeErr(w, http.StatusBadRequest, "нужен открытый ключ в одну строку: ssh-ed25519 AAAA… комментарий")
		return
	}
	hr, err := s.hostRequest(r, update.ActionSSH, "")
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	hr.SSHKey = req.Key
	if err := update.WriteJSON(filepath.Join(s.cfg.HostDir, update.RequestFile), hr); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	action := "system.ssh.enable"
	if req.Key == "" {
		action = "system.ssh.disable"
	}
	s.audit(r, action, "", nil)
	writeJSON(w, http.StatusAccepted, map[string]bool{"enabled": req.Key != ""})
}

func (s *Server) handlePower(w http.ResponseWriter, r *http.Request) {
	act := update.Action(r.PathValue("action"))
	if act != update.ActionReboot && act != update.ActionPoweroff {
		writeErr(w, http.StatusNotFound, "неизвестное действие")
		return
	}
	req, err := s.hostRequest(r, act, "")
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "system."+string(act), "", nil)
	writeJSON(w, http.StatusAccepted, req)
}

func (s *Server) stagingDir() string { return filepath.Join(s.cfg.HostDir, update.StagingDir) }

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"status": update.ReadStatus(s.cfg.HostDir), "current": s.cfg.Version}
	if m, err := update.ReadManifest(s.stagingDir()); err == nil {
		resp["staged"] = m
		resp["staged_cmp"] = update.CompareVersions(m.Version, s.cfg.Version)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleUpdateUpload принимает пакет потоком (тело запроса — сам файл) и
// распаковывает его в staging, проверяя суммы на лету. Multipart здесь не
// нужен и только заставил бы держать копию во временном файле.
func (s *Server) handleUpdateUpload(w http.ResponseWriter, r *http.Request) {
	st := update.ReadStatus(s.cfg.HostDir)
	if st.State == update.StateQueued || st.State == update.StateRunning {
		writeErr(w, http.StatusConflict, "идёт другая операция")
		return
	}
	dir := s.stagingDir()
	tmp := dir + ".upload"
	_ = os.RemoveAll(tmp)
	body := http.MaxBytesReader(w, r.Body, 4<<30)
	m, err := update.Extract(body, tmp)
	if err != nil {
		_, _ = io.Copy(io.Discard, body)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if update.CompareVersions(m.Version, s.cfg.Version) < 0 {
		_ = os.RemoveAll(tmp)
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("версия пакета %s старше установленной %s", m.Version, s.cfg.Version))
		return
	}
	if m.MinVersion != "" && update.CompareVersions(s.cfg.Version, m.MinVersion) < 0 {
		_ = os.RemoveAll(tmp)
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("пакет ставится поверх версии не ниже %s, сначала обновитесь до неё", m.MinVersion))
		return
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "update.upload", m.Version, nil)
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	m, err := update.ReadManifest(s.stagingDir())
	if err != nil {
		writeErr(w, http.StatusConflict, "пакет не загружен")
		return
	}
	req, err := s.hostRequest(r, update.ActionUpdate, m.Version)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "update.apply", m.Version, nil)
	writeJSON(w, http.StatusAccepted, req)
}

func (s *Server) handleUpdateDiscard(w http.ResponseWriter, r *http.Request) {
	st := update.ReadStatus(s.cfg.HostDir)
	if st.State == update.StateQueued || st.State == update.StateRunning {
		writeErr(w, http.StatusConflict, "идёт установка")
		return
	}
	_ = os.RemoveAll(s.stagingDir())
	s.audit(r, "update.discard", "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleZapretStrategies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, zapret.Strategies())
}

func (s *Server) handleZapretStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, zapret.ReadStatus(s.cfg.ZapretStatus))
}
