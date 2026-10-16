// Package store — всё, что rendol-api хранит в Postgres.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rendolnet/internal/netcfg"
)

var ErrNotFound = errors.New("store: не найдено")

type Store struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) Ping(ctx context.Context) error { return s.db.Ping(ctx) }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ---- пользователи ----

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool { return r == RoleAdmin || r == RoleOperator || r == RoleViewer }

// AtLeast — иерархия ролей: admin > operator > viewer.
func (r Role) AtLeast(min Role) bool {
	rank := map[Role]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}
	return rank[r] >= rank[min]
}

type User struct {
	ID           int64      `json:"id"`
	Username     string     `json:"username"`
	FullName     string     `json:"full_name"`
	Role         Role       `json:"role"`
	Disabled     bool       `json:"disabled"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLogin    *time.Time `json:"last_login,omitempty"`
	PasswordHash string     `json:"-"`
}

const userCols = `id, username, full_name, role, disabled, created_at, last_login, password_hash`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.FullName, &u.Role, &u.Disabled, &u.CreatedAt, &u.LastLogin, &u.PasswordHash); err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `select count(*) from users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	return s.db.QueryRow(ctx,
		`insert into users(username, full_name, password_hash, role) values($1,$2,$3,$4) returning id, created_at`,
		u.Username, u.FullName, u.PasswordHash, u.Role).Scan(&u.ID, &u.CreatedAt)
}

func (s *Store) UserByName(ctx context.Context, name string) (*User, error) {
	return scanUser(s.db.QueryRow(ctx, `select `+userCols+` from users where username=$1`, name))
}

func (s *Store) User(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRow(ctx, `select `+userCols+` from users where id=$1`, id))
}

func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.Query(ctx, `select `+userCols+` from users order by username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, u *User) error {
	tag, err := s.db.Exec(ctx, `update users set full_name=$2, role=$3, disabled=$4 where id=$1`,
		u.ID, u.FullName, u.Role, u.Disabled)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) SetPassword(ctx context.Context, id int64, hash string) error {
	_, err := s.db.Exec(ctx, `update users set password_hash=$2 where id=$1`, id, hash)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `delete from users where id=$1`, id)
	return err
}

func (s *Store) TouchLogin(ctx context.Context, id int64) {
	_, _ = s.db.Exec(ctx, `update users set last_login=now() where id=$1`, id)
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `select count(*) from users where role='admin' and not disabled`).Scan(&n)
	return n, err
}

// ---- токены API ----

type APIToken struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Store) CreateToken(ctx context.Context, userID int64, name string, hash []byte, exp *time.Time) (*APIToken, error) {
	t := &APIToken{UserID: userID, Name: name, ExpiresAt: exp}
	err := s.db.QueryRow(ctx, `insert into api_tokens(user_id, name, token_hash, expires_at) values($1,$2,$3,$4) returning id, created_at`,
		userID, name, hash, exp).Scan(&t.ID, &t.CreatedAt)
	return t, err
}

func (s *Store) Tokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.Query(ctx, `select id, user_id, name, created_at, last_used, expires_at from api_tokens where user_id=$1 order by id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.LastUsed, &t.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteToken(ctx context.Context, userID, id int64) error {
	tag, err := s.db.Exec(ctx, `delete from api_tokens where id=$1 and user_id=$2`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) UserByToken(ctx context.Context, hash []byte) (*User, error) {
	u, err := scanUser(s.db.QueryRow(ctx, `select `+prefixed("u", userCols)+` from api_tokens t join users u on u.id=t.user_id
		where t.token_hash=$1 and (t.expires_at is null or t.expires_at > now())`, hash))
	if err != nil {
		return nil, err
	}
	_, _ = s.db.Exec(ctx, `update api_tokens set last_used=now() where token_hash=$1`, hash)
	return u, nil
}

func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// ---- ревизии конфига ----

type RevState string

const (
	RevPending    RevState = "pending"
	RevCommitted  RevState = "committed"
	RevRolledBack RevState = "rolled_back"
	RevFailed     RevState = "failed"
	RevImported   RevState = "imported"
)

type Revision struct {
	ID              int64          `json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	Author          string         `json:"author"`
	Comment         string         `json:"comment"`
	State           RevState       `json:"state"`
	ConfirmDeadline *time.Time     `json:"confirm_deadline,omitempty"`
	Error           string         `json:"error,omitempty"`
	Body            *netcfg.Config `json:"body,omitempty"`
}

func (s *Store) InsertRevision(ctx context.Context, r *Revision) error {
	body, err := json.Marshal(r.Body)
	if err != nil {
		return err
	}
	return s.db.QueryRow(ctx, `insert into config_revisions(author, comment, body, state, confirm_deadline)
		values($1,$2,$3,$4,$5) returning id, created_at`,
		r.Author, r.Comment, body, r.State, r.ConfirmDeadline).Scan(&r.ID, &r.CreatedAt)
}

func (s *Store) SetRevisionState(ctx context.Context, id int64, st RevState, errText string) error {
	_, err := s.db.Exec(ctx, `update config_revisions set state=$2, error=$3,
		confirm_deadline = case when $2 = 'pending' then confirm_deadline else null end where id=$1`, id, st, errText)
	return err
}

func (s *Store) Revision(ctx context.Context, id int64) (*Revision, error) {
	var r Revision
	var body []byte
	err := s.db.QueryRow(ctx, `select id, created_at, author, comment, state, confirm_deadline, error, body
		from config_revisions where id=$1`, id).Scan(&r.ID, &r.CreatedAt, &r.Author, &r.Comment, &r.State, &r.ConfirmDeadline, &r.Error, &body)
	if err != nil {
		return nil, notFound(err)
	}
	r.Body = &netcfg.Config{}
	if err := json.Unmarshal(body, r.Body); err != nil {
		return nil, fmt.Errorf("store: ревизия %d: %w", id, err)
	}
	return &r, nil
}

func (s *Store) Revisions(ctx context.Context, limit, offset int) ([]Revision, int, error) {
	var total int
	if err := s.db.QueryRow(ctx, `select count(*) from config_revisions`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(ctx, `select id, created_at, author, comment, state, confirm_deadline, error
		from config_revisions order by id desc limit $1 offset $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Revision
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Author, &r.Comment, &r.State, &r.ConfirmDeadline, &r.Error); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// Running — последняя ревизия, которая сейчас действует на узле или ждёт
// подтверждения.
func (s *Store) Running(ctx context.Context) (*Revision, error) {
	var id int64
	err := s.db.QueryRow(ctx, `select id from config_revisions where state in ('committed','imported','pending')
		order by id desc limit 1`).Scan(&id)
	if err != nil {
		return nil, notFound(err)
	}
	return s.Revision(ctx, id)
}

func (s *Store) LastConfirmed(ctx context.Context) (*Revision, error) {
	var id int64
	err := s.db.QueryRow(ctx, `select id from config_revisions where state in ('committed','imported')
		order by id desc limit 1`).Scan(&id)
	if err != nil {
		return nil, notFound(err)
	}
	return s.Revision(ctx, id)
}

func (s *Store) PendingRevisions(ctx context.Context) ([]Revision, error) {
	rows, err := s.db.Query(ctx, `select id, created_at, author, comment, state, confirm_deadline, error
		from config_revisions where state='pending' order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Revision
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Author, &r.Comment, &r.State, &r.ConfirmDeadline, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- кандидат ----

type Candidate struct {
	Body         netcfg.Config `json:"body"`
	BaseRevision int64         `json:"base_revision"`
	UpdatedAt    time.Time     `json:"updated_at"`
	UpdatedBy    string        `json:"updated_by"`
}

func (s *Store) Candidate(ctx context.Context) (*Candidate, error) {
	var c Candidate
	var body []byte
	var base *int64
	err := s.db.QueryRow(ctx, `select body, base_revision, updated_at, updated_by from candidate where id=1`).
		Scan(&body, &base, &c.UpdatedAt, &c.UpdatedBy)
	if err != nil {
		return nil, notFound(err)
	}
	if base != nil {
		c.BaseRevision = *base
	}
	if err := json.Unmarshal(body, &c.Body); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) PutCandidate(ctx context.Context, body *netcfg.Config, base int64, by string) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `insert into candidate(id, body, base_revision, updated_at, updated_by) values(1,$1,$2,now(),$3)
		on conflict (id) do update set body=excluded.body, base_revision=excluded.base_revision,
		updated_at=excluded.updated_at, updated_by=excluded.updated_by`, b, base, by)
	return err
}

// ---- аудит ----

type AuditEntry struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	Username   string    `json:"username"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	Details    any       `json:"details,omitempty"`
	RemoteAddr string    `json:"remote_addr"`
}

func (s *Store) Audit(ctx context.Context, e AuditEntry) {
	var details []byte
	if e.Details != nil {
		details, _ = json.Marshal(e.Details)
	}
	_, _ = s.db.Exec(ctx, `insert into audit_log(username, action, target, details, remote_addr) values($1,$2,$3,$4,$5)`,
		e.Username, e.Action, e.Target, details, e.RemoteAddr)
}

func (s *Store) AuditLog(ctx context.Context, limit int, before int64) ([]AuditEntry, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.db.Query(ctx, `select id, at, username, action, target, details, remote_addr from audit_log
		where id < $1 order by id desc limit $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var details []byte
		if err := rows.Scan(&e.ID, &e.At, &e.Username, &e.Action, &e.Target, &details, &e.RemoteAddr); err != nil {
			return nil, err
		}
		if len(details) > 0 {
			var v any
			if json.Unmarshal(details, &v) == nil {
				e.Details = v
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- события ----

type Event struct {
	ID       int64      `json:"id"`
	At       time.Time  `json:"at"`
	Type     string     `json:"type"`
	Action   string     `json:"action"`
	Proto    int16      `json:"proto"`
	Src      netip.Addr `json:"src"`
	Dst      netip.Addr `json:"dst"`
	SrcPort  int32      `json:"sport"`
	DstPort  int32      `json:"dport"`
	FromZone string     `json:"from_zone"`
	ToZone   string     `json:"to_zone"`
	RuleID   string     `json:"rule_id"`
	Iface    string     `json:"iface"`
	Packets  int64      `json:"packets"`
	Bytes    int64      `json:"bytes"`
	Count    int32      `json:"count"`
}

func (s *Store) InsertEvents(ctx context.Context, evs []Event) error {
	if len(evs) == 0 {
		return nil
	}
	_, err := s.db.CopyFrom(ctx, pgx.Identifier{"events"},
		[]string{"at", "type", "action", "proto", "src", "dst", "sport", "dport", "from_zone", "to_zone", "rule_id", "iface", "packets", "bytes", "count"},
		pgx.CopyFromSlice(len(evs), func(i int) ([]any, error) {
			e := evs[i]
			return []any{e.At, e.Type, e.Action, e.Proto, e.Src, e.Dst, e.SrcPort, e.DstPort,
				e.FromZone, e.ToZone, e.RuleID, e.Iface, e.Packets, e.Bytes, e.Count}, nil
		}))
	return err
}

type EventFilter struct {
	Type   string
	Action string
	Addr   string // источник или назначение, адрес или сеть
	Port   int
	RuleID string
	Since  time.Time
	Until  time.Time
	Before int64
	Limit  int
}

func (s *Store) Events(ctx context.Context, f EventFilter) ([]Event, error) {
	var where []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Type != "" {
		where = append(where, "type="+arg(f.Type))
	}
	if f.Action != "" {
		where = append(where, "action="+arg(f.Action))
	}
	if f.Addr != "" {
		p, err := netcfg.ParseAddrOrPrefix(f.Addr)
		if err != nil {
			return nil, err
		}
		a := arg(p.String())
		where = append(where, fmt.Sprintf("(src <<= %s::inet or dst <<= %s::inet)", a, a))
	}
	if f.Port > 0 {
		a := arg(f.Port)
		where = append(where, fmt.Sprintf("(sport=%s or dport=%s)", a, a))
	}
	if f.RuleID != "" {
		where = append(where, "rule_id="+arg(f.RuleID))
	}
	if !f.Since.IsZero() {
		where = append(where, "at >= "+arg(f.Since))
	}
	if !f.Until.IsZero() {
		where = append(where, "at <= "+arg(f.Until))
	}
	if f.Before > 0 {
		where = append(where, "id < "+arg(f.Before))
	}
	q := `select id, at, type, action, proto, src, dst, sport, dport, from_zone, to_zone, rule_id, iface, packets, bytes, count from events`
	if len(where) > 0 {
		q += " where " + strings.Join(where, " and ")
	}
	if f.Limit <= 0 || f.Limit > 5000 {
		f.Limit = 500
	}
	q += " order by id desc limit " + arg(f.Limit)
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.At, &e.Type, &e.Action, &e.Proto, &e.Src, &e.Dst, &e.SrcPort, &e.DstPort,
			&e.FromZone, &e.ToZone, &e.RuleID, &e.Iface, &e.Packets, &e.Bytes, &e.Count); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type TopEntry struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// TopDenied — самые частые источники и порты отбоев за период.
func (s *Store) TopDenied(ctx context.Context, since time.Time, limit int) (srcs, ports []TopEntry, err error) {
	q := func(sql string) ([]TopEntry, error) {
		rows, err := s.db.Query(ctx, sql, since, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []TopEntry
		for rows.Next() {
			var e TopEntry
			if err := rows.Scan(&e.Key, &e.Count); err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, rows.Err()
	}
	srcs, err = q(`select host(src), sum(count) c from events where type='deny' and at >= $1 group by src order by c desc limit $2`)
	if err != nil {
		return nil, nil, err
	}
	ports, err = q(`select case proto when 6 then 'tcp/' || dport when 17 then 'udp/' || dport when 1 then 'icmp'
		when 58 then 'icmpv6' when 2 then 'igmp' else 'ip/' || proto end, sum(count) c
		from events where type='deny' and at >= $1 group by proto, dport order by c desc limit $2`)
	return srcs, ports, err
}

func (s *Store) PruneEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, `delete from events where at < $1`, before)
	return tag.RowsAffected(), err
}

// ---- трафик ----

type TrafficPoint struct {
	At    time.Time `json:"at"`
	Iface string    `json:"iface"`
	RxBps int64     `json:"rx_bps"`
	TxBps int64     `json:"tx_bps"`
}

func (s *Store) InsertTraffic(ctx context.Context, pts []TrafficPoint) error {
	b := &pgx.Batch{}
	for _, p := range pts {
		b.Queue(`insert into traffic(at, iface, rx_bps, tx_bps) values($1,$2,$3,$4) on conflict do nothing`, p.At, p.Iface, p.RxBps, p.TxBps)
	}
	return s.db.SendBatch(ctx, b).Close()
}

func (s *Store) Traffic(ctx context.Context, since time.Time) ([]TrafficPoint, error) {
	rows, err := s.db.Query(ctx, `select at, iface, rx_bps, tx_bps from traffic where at >= $1 order by at`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficPoint
	for rows.Next() {
		var p TrafficPoint
		if err := rows.Scan(&p.At, &p.Iface, &p.RxBps, &p.TxBps); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PruneTraffic(ctx context.Context, before time.Time) error {
	_, err := s.db.Exec(ctx, `delete from traffic where at < $1`, before)
	return err
}

// ---- настройки ----

func (s *Store) Setting(ctx context.Context, key string, v any) error {
	var b []byte
	if err := s.db.QueryRow(ctx, `select value from settings where key=$1`, key).Scan(&b); err != nil {
		return notFound(err)
	}
	return json.Unmarshal(b, v)
}

func (s *Store) PutSetting(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `insert into settings(key, value) values($1,$2) on conflict (key) do update set value=excluded.value`, key, b)
	return err
}
