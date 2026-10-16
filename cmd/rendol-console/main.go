// rendol-console — экран на tty1 установленного узла: адрес веб-интерфейса,
// состояние служб и то, что нельзя потерять из веба: сеть управления и
// сброс пароля администратора. Физический доступ к консоли считается
// достаточным правом.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rendolnet/internal/installer"
)

var version = "dev"

const (
	mgmtUnit  = "/etc/systemd/network/10-mgmt.network"
	hostDir   = "/var/lib/rendolnet/host"
	agentSock = "/run/rendolnet/agent.sock"
)

var (
	sTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208"))
	sDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	sOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	sBad   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	sSel   = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	sBig   = lipgloss.NewStyle().Bold(true).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("208")).Padding(0, 2)
)

type status struct {
	host      string
	iface     string
	addrs     []string
	k3s       string
	api       string
	apiVer    string
	agentRev  int64
	agentErr  string
	installed string
}

func probe() status {
	s := status{}
	s.host, _ = os.Hostname()
	if b, err := os.ReadFile(mgmtUnit); err == nil {
		if m := regexp.MustCompile(`(?m)^Name=(\S+)`).FindSubmatch(b); m != nil {
			s.iface = string(m[1])
		}
	}
	if it, err := net.InterfaceByName(s.iface); err == nil {
		addrs, _ := it.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				s.addrs = append(s.addrs, n.IP.String())
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "systemctl", "is-active", "k3s").Output()
	s.k3s = strings.TrimSpace(string(out))

	hc := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1/healthz", nil)
	if resp, err := hc.Do(req); err == nil {
		var v struct{ Version string }
		_ = json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		s.api, s.apiVer = "ok", v.Version
	} else {
		s.api = "нет ответа"
	}

	ac := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", agentSock)
		},
	}}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/v1/status", nil)
	if resp, err := ac.Do(req); err == nil {
		var v struct {
			Revision  int64  `json:"revision"`
			LastError string `json:"last_error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		s.agentRev, s.agentErr = v.Revision, v.LastError
	} else {
		s.agentErr = "агент не отвечает"
	}
	if b, err := os.ReadFile(filepath.Join(hostDir, "version")); err == nil {
		s.installed = strings.TrimSpace(string(b))
	}
	return s
}

type mode int

const (
	mMenu mode = iota
	mNet
	mConfirm
)

type tickMsg struct{}
type statusMsg status

type model struct {
	st      status
	mode    mode
	cur     int
	msg     string
	confirm string
	dhcp    bool
	fields  []string
	fcur    int
}

var menu = []string{
	"Сеть управления",
	"Сбросить пароль admin",
	"Перезагрузить",
	"Выключить",
	"Командная оболочка",
}

func refresh() tea.Cmd {
	return func() tea.Msg { return statusMsg(probe()) }
}

func tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) Init() tea.Cmd { return tea.Batch(refresh(), tick()) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Batch(refresh(), tick())
	case statusMsg:
		m.st = status(msg)
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case mMenu:
		switch k.String() {
		case "up":
			if m.cur > 0 {
				m.cur--
			}
		case "down":
			if m.cur < len(menu)-1 {
				m.cur++
			}
		case "enter":
			m.msg = ""
			switch m.cur {
			case 0:
				m.mode = mNet
				m.dhcp = true
				m.fields = []string{"", "", ""}
				m.fcur = 0
			case 1, 2, 3:
				m.mode = mConfirm
				m.confirm = menu[m.cur]
			case 4:
				c := exec.CommandContext(context.Background(), "/bin/bash", "-l")
				return m, tea.ExecProcess(c, func(error) tea.Msg { return tickMsg{} })
			}
		}
	case mConfirm:
		switch k.String() {
		case "y", "Y", "д", "Д":
			m.mode = mMenu
			switch m.confirm {
			case menu[1]:
				if err := os.WriteFile(filepath.Join(hostDir, "reset-admin"), nil, 0o600); err != nil {
					m.msg = sBad.Render(err.Error())
				} else {
					m.msg = sOK.Render("Пароль admin будет сброшен на «admin» в течение нескольких секунд.")
				}
			case menu[2]:
				_ = exec.CommandContext(context.Background(), "systemctl", "reboot").Run()
			case menu[3]:
				_ = exec.CommandContext(context.Background(), "systemctl", "poweroff").Run()
			}
		default:
			m.mode = mMenu
		}
	case mNet:
		switch k.Type {
		case tea.KeyEsc:
			m.mode = mMenu
		case tea.KeyF2:
			m.dhcp = !m.dhcp
		case tea.KeyTab, tea.KeyDown:
			m.fcur = (m.fcur + 1) % len(m.fields)
		case tea.KeyShiftTab, tea.KeyUp:
			m.fcur = (m.fcur + len(m.fields) - 1) % len(m.fields)
		case tea.KeyBackspace:
			if f := m.fields[m.fcur]; len(f) > 0 {
				m.fields[m.fcur] = f[:len(f)-1]
			}
		case tea.KeyRunes:
			m.fields[m.fcur] += string(k.Runes)
		case tea.KeyEnter:
			m.msg = applyNet(m.st.iface, m.dhcp, m.fields)
			m.mode = mMenu
			return m, refresh()
		}
	}
	return m, nil
}

func applyNet(iface string, dhcp bool, f []string) string {
	if iface == "" {
		return sBad.Render("не найден интерфейс управления в " + mgmtUnit)
	}
	a := installer.Addr{DHCP: dhcp}
	if !dhcp {
		a.Address, a.Gateway, a.DNS = strings.TrimSpace(f[0]), strings.TrimSpace(f[1]), strings.Fields(f[2])
		probeAnswers := installer.Answers{Hostname: "x", Mgmt: iface, MgmtAddr: a}
		if err := probeAnswers.Validate(); err != nil {
			return sBad.Render(err.Error())
		}
	}
	if err := os.WriteFile(mgmtUnit, []byte(installer.MgmtNetworkUnit(iface, a)), 0o644); err != nil {
		return sBad.Render(err.Error())
	}
	_ = exec.CommandContext(context.Background(), "networkctl", "reload").Run()
	_ = exec.CommandContext(context.Background(), "networkctl", "reconfigure", iface).Run()
	return sOK.Render("Сеть управления перенастроена.")
}

func (m model) View() string {
	var b strings.Builder
	st := m.st
	b.WriteString(sTitle.Render("RendolNet") + sDim.Render(fmt.Sprintf("  %s · %s", st.host, nonEmpty(st.installed, version))) + "\n\n")

	url := "адрес не получен"
	if len(st.addrs) > 0 {
		url = "https://" + st.addrs[0] + "/"
	}
	b.WriteString(sBig.Render(url) + "\n")
	b.WriteString(sDim.Render("интерфейс управления: "+nonEmpty(st.iface, "—")) + "\n\n")

	line := func(name, val string, ok bool) {
		v := sBad.Render(val)
		if ok {
			v = sOK.Render(val)
		}
		fmt.Fprintf(&b, "  %-16s %s\n", name, v)
	}
	line("k3s", nonEmpty(st.k3s, "—"), st.k3s == "active")
	line("веб-интерфейс", strings.TrimSpace(st.api+" "+st.apiVer), st.api == "ok")
	if st.agentErr != "" {
		line("dataplane", st.agentErr, false)
	} else {
		line("dataplane", fmt.Sprintf("ревизия r%d", st.agentRev), true)
	}
	b.WriteString("\n")

	switch m.mode {
	case mMenu:
		for i, it := range menu {
			if i == m.cur {
				b.WriteString(sSel.Render("› "+it) + "\n")
			} else {
				b.WriteString("  " + it + "\n")
			}
		}
		b.WriteString(sDim.Render("\n↑↓ выбор · Enter"))
	case mConfirm:
		fmt.Fprintf(&b, "%s? %s\n", m.confirm, sSel.Render("y")+sDim.Render(" — да, любая клавиша — нет"))
	case mNet:
		mode := "статический адрес"
		if m.dhcp {
			mode = "DHCP"
		}
		fmt.Fprintf(&b, "Сеть управления · %s · %s %s\n\n", st.iface, sSel.Render(mode), sDim.Render("(F2 — сменить)"))
		if !m.dhcp {
			for i, l := range []string{"Адрес/маска", "Шлюз", "DNS"} {
				cur, val := "  ", m.fields[i]
				if i == m.fcur {
					cur, val = sSel.Render("› "), val+sSel.Render("▏")
				}
				fmt.Fprintf(&b, "%s%-12s %s\n", cur, l, val)
			}
		}
		b.WriteString(sDim.Render("\nEnter применить · Esc отмена"))
	}
	if m.msg != "" {
		b.WriteString("\n\n" + m.msg)
	}
	return lipgloss.NewStyle().Padding(1, 3).Render(b.String())
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	if _, err := tea.NewProgram(model{}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
