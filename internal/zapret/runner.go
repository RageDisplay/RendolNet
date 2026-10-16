package zapret

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rendolnet/internal/netcfg"
)

const (
	QueueNum = 200
	// метка пакетов, которые nfqws отправляет сам: правило их пропускает,
	// иначе подделанный пакет снова попадёт в очередь
	DesyncMark = 0x40000000
	Table      = "rendol_zapret"
)

type Runner struct {
	Log       *slog.Logger
	AgentSock string
	Nfqws     string // /app/nfqws
	BaseDir   string // /opt/zapret: bin/ и lists/ из релиза Flowseal
	WorkDir   string // /run/zapret
	StatusOut string // /run/rendolnet/zapret.json

	mu      sync.Mutex
	proc    *proc
	applied string
	status  Status
}

type proc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (p *proc) alive() bool {
	if p == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

type Status struct {
	Enabled  bool      `json:"enabled"`
	Running  bool      `json:"running"`
	Strategy string    `json:"strategy,omitempty"`
	Iface    string    `json:"iface,omitempty"`
	Started  time.Time `json:"started,omitempty"`
	Restarts int       `json:"restarts"`
	Error    string    `json:"error,omitempty"`
}

func (r *Runner) Run(ctx context.Context) error {
	defer r.stop()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		if err := r.reconcile(ctx); err != nil {
			r.Log.Warn("zapret", "err", err)
			r.setErr(err.Error())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (r *Runner) fetch(ctx context.Context) (*netcfg.Config, error) {
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", r.AgentSock)
		},
	}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/v1/config", nil)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("агент ответил %s", resp.Status)
	}
	var ap struct {
		Config netcfg.Config `json:"config"`
	}
	return &ap.Config, json.NewDecoder(resp.Body).Decode(&ap)
}

func (r *Runner) reconcile(ctx context.Context) error {
	c, err := r.fetch(ctx)
	if err != nil {
		return err
	}
	z := c.Zapret
	key, _ := json.Marshal(z)
	h := sha256.Sum256(key)
	sum := hex.EncodeToString(h[:])

	r.mu.Lock()
	alive := r.proc.alive()
	same := sum == r.applied
	r.mu.Unlock()
	if same && (alive || !z.Enabled) {
		return nil
	}

	r.stop()
	if !z.Enabled {
		r.applied = sum
		r.setStatus(Status{})
		return nil
	}
	s, ok := Find(z.Strategy)
	if !ok {
		return fmt.Errorf("стратегия %q не найдена", z.Strategy)
	}
	if !s.Compatible {
		return fmt.Errorf("стратегия %q не переносится на Linux", z.Strategy)
	}
	lists, err := PrepareLists(r.BaseDir, r.WorkDir, z)
	if err != nil {
		return err
	}
	if err := nft(Rules(z.Interface, s)); err != nil {
		return fmt.Errorf("nftables: %w", err)
	}
	args := append([]string{fmt.Sprintf("--qnum=%d", QueueNum), fmt.Sprintf("--dpi-desync-fwmark=0x%x", DesyncMark)},
		s.Expand(filepath.Join(r.BaseDir, "bin"), lists, z.Fooling)...)
	// процесс живёт дольше вызова reconcile и останавливается в stop()
	cmd := exec.CommandContext(context.Background(), r.Nfqws, args...)
	cmd.Stdout = logWriter{r.Log}
	cmd.Stderr = logWriter{r.Log}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("запуск nfqws: %w", err)
	}
	pr := &proc{cmd: cmd, done: make(chan struct{})}
	r.mu.Lock()
	r.proc = pr
	r.applied = sum
	prev := r.status.Restarts
	if same {
		prev++
	}
	r.status = Status{Enabled: true, Running: true, Strategy: s.Name, Iface: z.Interface, Started: time.Now(), Restarts: prev}
	r.mu.Unlock()
	r.writeStatus()
	r.Log.Info("nfqws запущен", "strategy", s.Name, "iface", z.Interface)
	go func() {
		err := cmd.Wait()
		close(pr.done)
		r.Log.Warn("nfqws завершился", "err", err)
		r.mu.Lock()
		r.status.Running = false
		if err != nil {
			r.status.Error = err.Error()
		}
		r.mu.Unlock()
		r.writeStatus()
	}()
	return nil
}

// Rules — таблица nftables: первые пакеты исходящих соединений на порты
// стратегии уходят в очередь nfqws. bypass: если nfqws не запущен, пакеты
// идут дальше, а не теряются.
func Rules(iface string, s Strategy) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %[1]s\ndelete table inet %[1]s\ntable inet %[1]s {\n", Table)
	b.WriteString("\tchain post {\n\t\ttype filter hook postrouting priority mangle; policy accept;\n")
	if len(s.TCPPorts) > 0 {
		fmt.Fprintf(&b, "\t\toifname %q meta mark & 0x%x == 0 tcp dport { %s } ct original packets 1-6 queue flags bypass to %d\n",
			iface, DesyncMark, strings.Join(s.TCPPorts, ", "), QueueNum)
	}
	if len(s.UDPPorts) > 0 {
		fmt.Fprintf(&b, "\t\toifname %q meta mark & 0x%x == 0 udp dport { %s } ct original packets 1-6 queue flags bypass to %d\n",
			iface, DesyncMark, strings.Join(s.UDPPorts, ", "), QueueNum)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

func nft(script string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PrepareLists копирует списки релиза в рабочий каталог и дописывает свои.
// Пустые пользовательские списки nfqws не принимает, поэтому в них стоит
// заглушка, как в сборке Flowseal.
func PrepareLists(baseDir, workDir string, z netcfg.Zapret) (string, error) {
	dir := filepath.Join(workDir, "lists")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(filepath.Join(baseDir, "lists"))
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if err := copyFile(filepath.Join(baseDir, "lists", e.Name()), filepath.Join(dir, e.Name())); err != nil {
			return "", err
		}
	}
	write := func(name string, lines []string, placeholder string) error {
		if len(lines) == 0 {
			lines = []string{placeholder}
		}
		return os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
	if err := write("list-general-user.txt", z.Hosts, "domain.example.abc"); err != nil {
		return "", err
	}
	for _, f := range []string{"list-exclude-user.txt"} {
		if err := write(f, nil, "domain.example.abc"); err != nil {
			return "", err
		}
	}
	if err := write("ipset-exclude-user.txt", nil, "203.0.113.113/32"); err != nil {
		return "", err
	}
	if len(z.IPs) > 0 {
		if err := write("ipset-all.txt", z.IPs, ""); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func (r *Runner) stop() {
	r.mu.Lock()
	pr := r.proc
	r.proc = nil
	r.mu.Unlock()
	_ = nft(fmt.Sprintf("table inet %[1]s\ndelete table inet %[1]s\n", Table))
	if !pr.alive() {
		return
	}
	_ = pr.cmd.Process.Signal(os.Interrupt)
	select {
	case <-pr.done:
	case <-time.After(3 * time.Second):
		_ = pr.cmd.Process.Kill()
		<-pr.done
	}
}

func (r *Runner) setErr(msg string) {
	r.mu.Lock()
	r.status.Error = msg
	r.mu.Unlock()
	r.writeStatus()
}

func (r *Runner) setStatus(s Status) {
	r.mu.Lock()
	r.status = s
	r.mu.Unlock()
	r.writeStatus()
}

func (r *Runner) writeStatus() {
	if r.StatusOut == "" {
		return
	}
	r.mu.Lock()
	b, _ := json.Marshal(r.status)
	r.mu.Unlock()
	tmp := r.StatusOut + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, r.StatusOut)
	}
}

func ReadStatus(path string) Status {
	var s Status
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type logWriter struct{ l *slog.Logger }

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			w.l.Info("nfqws", "out", line)
		}
	}
	return len(p), nil
}
