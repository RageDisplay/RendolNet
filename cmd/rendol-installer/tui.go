package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rendolnet/internal/installer"
)

var (
	accent = lipgloss.Color("208")
	dim    = lipgloss.Color("244")
	bad    = lipgloss.Color("203")
	good   = lipgloss.Color("78")

	sTitle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	sDim   = lipgloss.NewStyle().Foreground(dim)
	sBad   = lipgloss.NewStyle().Foreground(bad)
	sGood  = lipgloss.NewStyle().Foreground(good)
	sSel   = lipgloss.NewStyle().Foreground(accent).Bold(true)
	sBox   = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(dim).Padding(1, 3)
)

type screen int

const (
	scDisk screen = iota
	scConfirmDisk
	scNICs
	scMgmt
	scWAN
	scLAN
	scHost
	scSummary
	scRun
	scDone
)

// field — однострочное поле ввода; bubbles сюда не тянем ради одного виджета.
type field struct {
	label string
	value string
}

type form struct {
	fields []field
	cur    int
}

func (f *form) key(k tea.KeyMsg) {
	switch k.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		if f.cur > 0 {
			f.cur--
		}
	case tea.KeyDown, tea.KeyTab:
		if f.cur < len(f.fields)-1 {
			f.cur++
		}
	case tea.KeyBackspace:
		v := []rune(f.fields[f.cur].value)
		if len(v) > 0 {
			f.fields[f.cur].value = string(v[:len(v)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		f.fields[f.cur].value += string(k.Runes)
	}
}

func (f *form) view() string {
	var b strings.Builder
	for i, fl := range f.fields {
		cursor := "  "
		val := fl.value
		if i == f.cur {
			cursor = sSel.Render("› ")
			val += sSel.Render("▏")
		}
		fmt.Fprintf(&b, "%s%-22s %s\n", cursor, fl.label, val)
	}
	return b.String()
}

type progressMsg struct {
	i, total int
	title    string
}
type doneMsg struct{ err error }

type model struct {
	e      *env
	disks  []installer.Disk
	nics   []installer.NIC
	roles  []int // 0 —, 1 mgmt, 2 wan, 3 lan
	a      installer.Answers
	sc     screen
	cur    int
	err    string
	input  string
	dhcp   bool
	f      form
	prog   progressMsg
	runErr error
	events chan tea.Msg
	w, h   int
}

var roleNames = []string{"—", "управление", "WAN", "LAN"}

func newModel(e *env, disks []installer.Disk, nics []installer.NIC) model {
	m := model{e: e, disks: disks, nics: nics, roles: make([]int, len(nics)), events: make(chan tea.Msg, 16)}
	m.a = installer.Answers{Language: "ru", Hostname: "rendolnet", Timezone: "Europe/Moscow", LANAddr: "192.168.1.1/24"}
	// разумное начальное распределение: первая карта — управление, вторая — WAN, третья — LAN
	for i := range nics {
		if i < 3 {
			m.roles[i] = i + 1
		}
	}
	m.dhcp = true
	return m
}

func (m model) Init() tea.Cmd { return nil }

func (m model) waitEvent() tea.Cmd {
	return func() tea.Msg { return <-m.events }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case progressMsg:
		m.prog = msg
		return m, m.waitEvent()
	case doneMsg:
		m.runErr = msg.err
		m.sc = scDone
		return m, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC && m.sc != scRun {
			return m, tea.Quit
		}
		return m.key(msg)
	}
	return m, nil
}

func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = ""
	switch m.sc {
	case scDisk:
		switch k.String() {
		case "up":
			if m.cur > 0 {
				m.cur--
			}
		case "down":
			if m.cur < len(m.disks)-1 {
				m.cur++
			}
		case "enter":
			if len(m.disks) == 0 {
				return m, nil
			}
			d := m.disks[m.cur]
			switch d.State {
			case installer.DiskBlocked:
				m.err = "Этот диск нельзя использовать: " + d.Reason
			case installer.DiskHasData:
				m.a.Disk = d.Path
				m.input = ""
				m.sc = scConfirmDisk
			default:
				m.a.Disk = d.Path
				m.sc = scNICs
				m.cur = 0
			}
		}

	case scConfirmDisk:
		d, _ := installer.FindDisk(m.disks, m.a.Disk)
		switch k.Type {
		case tea.KeyEsc:
			m.sc = scDisk
		case tea.KeyEnter:
			if err := installer.CheckTarget(d, m.input); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.a.Confirm = m.input
			m.sc = scNICs
			m.cur = 0
		case tea.KeyBackspace:
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case tea.KeyRunes:
			m.input += string(k.Runes)
		}

	case scNICs:
		switch k.String() {
		case "up":
			if m.cur > 0 {
				m.cur--
			}
		case "down":
			if m.cur < len(m.nics)-1 {
				m.cur++
			}
		case "left":
			m.roles[m.cur] = (m.roles[m.cur] + 3) % 4
		case "right", " ":
			m.roles[m.cur] = (m.roles[m.cur] + 1) % 4
		case "esc":
			m.sc = scDisk
		case "enter":
			m.a.Mgmt, m.a.WAN, m.a.LAN = "", "", ""
			count := map[int]int{}
			for i, r := range m.roles {
				count[r]++
				switch r {
				case 1:
					m.a.Mgmt = m.nics[i].Name
				case 2:
					m.a.WAN = m.nics[i].Name
				case 3:
					m.a.LAN = m.nics[i].Name
				}
			}
			if count[1] != 1 || count[2] > 1 || count[3] > 1 {
				m.err = "Нужен ровно один интерфейс управления, WAN и LAN — не больше одного"
				return m, nil
			}
			m.dhcp = m.a.MgmtAddr.DHCP || m.a.MgmtAddr.Address == ""
			m.f = form{fields: []field{{"Адрес/маска", m.a.MgmtAddr.Address}, {"Шлюз", m.a.MgmtAddr.Gateway}, {"DNS", strings.Join(m.a.MgmtAddr.DNS, " ")}}}
			m.sc = scMgmt
		}

	case scMgmt, scWAN:
		switch k.Type {
		case tea.KeyEsc:
			m.sc = scNICs
			return m, nil
		case tea.KeyF2:
			m.dhcp = !m.dhcp
			return m, nil
		case tea.KeyEnter:
			addr := installer.Addr{DHCP: m.dhcp}
			if !m.dhcp {
				addr.Address = strings.TrimSpace(m.f.fields[0].value)
				addr.Gateway = strings.TrimSpace(m.f.fields[1].value)
				addr.DNS = strings.Fields(m.f.fields[2].value)
			}
			if m.sc == scMgmt {
				m.a.MgmtAddr = addr
				if m.a.WAN != "" {
					m.dhcp = m.a.WANAddr.DHCP || m.a.WANAddr.Address == ""
					m.f = form{fields: []field{{"Адрес/маска", m.a.WANAddr.Address}, {"Шлюз", m.a.WANAddr.Gateway}, {"DNS", strings.Join(m.a.WANAddr.DNS, " ")}}}
					m.sc = scWAN
					return m, nil
				}
			} else {
				m.a.WANAddr = addr
			}
			return m.afterWAN(), nil
		}
		if !m.dhcp {
			m.f.key(k)
		}

	case scLAN:
		switch k.Type {
		case tea.KeyEsc:
			m.sc = scNICs
		case tea.KeyEnter:
			m.a.LANAddr = strings.TrimSpace(m.f.fields[0].value)
			m.f = form{fields: []field{{"Имя узла", m.a.Hostname}, {"Часовой пояс", m.a.Timezone}, {"Ключ SSH (необяз.)", m.a.SSHKey}}}
			m.sc = scHost
		default:
			m.f.key(k)
		}

	case scHost:
		switch k.Type {
		case tea.KeyEsc:
			m.sc = scNICs
		case tea.KeyEnter:
			m.a.Hostname = m.f.fields[0].value
			m.a.Timezone = m.f.fields[1].value
			m.a.SSHKey = m.f.fields[2].value
			if err := m.a.Validate(); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.sc = scSummary
		default:
			m.f.key(k)
		}

	case scSummary:
		switch k.String() {
		case "esc":
			m.sc = scHost
		case "y", "Y", "д", "Д":
			d, _ := installer.FindDisk(m.disks, m.a.Disk)
			steps, err := installer.Plan(m.a, d)
			if err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.sc = scRun
			ev := m.events
			go func() {
				err := m.e.execute(context.Background(), steps, func(i, total int, title string) {
					ev <- progressMsg{i, total, title}
				})
				ev <- doneMsg{err}
			}()
			return m, m.waitEvent()
		}

	case scDone:
		if k.Type == tea.KeyEnter {
			if m.runErr == nil {
				m.e.reboot()
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) afterWAN() model {
	if m.a.LAN != "" {
		m.f = form{fields: []field{{"Адрес/маска LAN", m.a.LANAddr}}}
		m.sc = scLAN
		return m
	}
	m.f = form{fields: []field{{"Имя узла", m.a.Hostname}, {"Часовой пояс", m.a.Timezone}, {"Ключ SSH (необяз.)", m.a.SSHKey}}}
	m.sc = scHost
	return m
}

func gib(n uint64) string { return fmt.Sprintf("%.1f ГиБ", float64(n)/(1<<30)) }

func (m model) View() string {
	var b strings.Builder
	b.WriteString(sTitle.Render("RendolNet") + sDim.Render("  установка  "+version) + "\n\n")
	switch m.sc {
	case scDisk:
		b.WriteString("Системный диск. Всё содержимое выбранного диска будет удалено.\n\n")
		if len(m.disks) == 0 {
			b.WriteString(sBad.Render("Дисков не найдено.") + "\n")
		}
		for i, d := range m.disks {
			mark := "  "
			line := fmt.Sprintf("%-8s %10s  %-24s", d.Name, gib(d.SizeBytes), d.Model)
			switch d.State {
			case installer.DiskBlocked:
				line = sDim.Render(line + "  " + d.Reason)
			case installer.DiskHasData:
				line += "  " + sBad.Render("есть данные: "+d.Reason)
			default:
				line += "  " + sGood.Render("пустой")
			}
			if i == m.cur {
				mark = sSel.Render("› ")
			}
			b.WriteString(mark + line + "\n")
		}
		b.WriteString(sDim.Render("\n↑↓ выбор · Enter далее · Ctrl+C выход"))

	case scConfirmDisk:
		d, _ := installer.FindDisk(m.disks, m.a.Disk)
		fmt.Fprintf(&b, "На %s есть данные: %s\n\n", d.Name, d.Reason)
		what := "серийный номер"
		if d.Serial == "" {
			what = "размер в байтах"
		}
		fmt.Fprintf(&b, "Чтобы стереть диск, введите его %s (%s):\n\n  %s%s\n", what, sDim.Render(installer.ConfirmationFor(d)), m.input, sSel.Render("▏"))
		b.WriteString(sDim.Render("\nEnter подтвердить · Esc назад"))

	case scNICs:
		b.WriteString("Роли сетевых портов. Управление — обязательно, WAN и LAN — по одному.\n\n")
		for i, n := range m.nics {
			mark := "  "
			link := sDim.Render("нет линка")
			if n.Carrier {
				link = sGood.Render("линк")
			}
			role := fmt.Sprintf("[%s]", roleNames[m.roles[i]])
			if i == m.cur {
				mark = sSel.Render("› ")
				role = sSel.Render(role)
			}
			fmt.Fprintf(&b, "%s%-12s %-17s %-10s %-9s %s\n", mark, n.Name, n.MAC, n.Driver, link, role)
		}
		if len(m.nics) == 0 {
			b.WriteString(sBad.Render("Сетевых карт не найдено.") + "\n")
		}
		b.WriteString(sDim.Render("\n↑↓ порт · ←→ роль · Enter далее · Esc назад"))

	case scMgmt, scWAN:
		name, title := m.a.Mgmt, "Сеть управления"
		if m.sc == scWAN {
			name, title = m.a.WAN, "Внешний канал (WAN)"
		}
		fmt.Fprintf(&b, "%s · %s\n\n", title, name)
		mode := "статический адрес"
		if m.dhcp {
			mode = "DHCP"
		}
		fmt.Fprintf(&b, "Режим: %s  %s\n\n", sSel.Render(mode), sDim.Render("(F2 — сменить)"))
		if !m.dhcp {
			b.WriteString(m.f.view())
		}
		if m.sc == scMgmt {
			b.WriteString(sDim.Render("\nНа этом адресе будет веб-интерфейс. Шлюз используется, только если нет WAN.\n"))
		}
		b.WriteString(sDim.Render("\nTab поле · Enter далее · Esc назад"))

	case scLAN:
		fmt.Fprintf(&b, "Внутренняя сеть (LAN) · %s\n\n", m.a.LAN)
		b.WriteString(m.f.view())
		b.WriteString(sDim.Render("\nСети LAN по умолчанию разрешён выход в интернет через маскарад.\n\nEnter далее · Esc назад"))

	case scHost:
		b.WriteString("Узел\n\n")
		b.WriteString(m.f.view())
		b.WriteString(sDim.Render("\nTab поле · Enter далее · Esc назад"))

	case scSummary:
		d, _ := installer.FindDisk(m.disks, m.a.Disk)
		addr := func(a installer.Addr) string {
			if a.DHCP {
				return "DHCP"
			}
			s := a.Address
			if a.Gateway != "" {
				s += " через " + a.Gateway
			}
			return s
		}
		rows := [][2]string{
			{"Диск", fmt.Sprintf("%s, %s %s", d.Name, gib(d.SizeBytes), d.Model)},
			{"Имя узла", m.a.Hostname},
			{"Управление", m.a.Mgmt + "  " + addr(m.a.MgmtAddr)},
		}
		if m.a.WAN != "" {
			rows = append(rows, [2]string{"WAN", m.a.WAN + "  " + addr(m.a.WANAddr)})
		}
		if m.a.LAN != "" {
			rows = append(rows, [2]string{"LAN", m.a.LAN + "  " + m.a.LANAddr})
		}
		rows = append(rows, [2]string{"Часовой пояс", m.a.Timezone})
		var s strings.Builder
		for _, r := range rows {
			fmt.Fprintf(&s, "%-14s %s\n", sDim.Render(r[0]), r[1])
		}
		b.WriteString(sBox.Render(strings.TrimRight(s.String(), "\n")) + "\n\n")
		b.WriteString(sBad.Render(fmt.Sprintf("Диск %s будет полностью стёрт.", d.Name)) + "\n\n")
		b.WriteString("Начать установку? " + sSel.Render("y") + sDim.Render(" — да, Esc — назад"))

	case scRun:
		total := m.prog.total
		if total == 0 {
			total = 1
		}
		width := 46
		filled := width * m.prog.i / total
		fmt.Fprintf(&b, "Установка…\n\n%s%s  %d/%d\n\n%s\n", sSel.Render(strings.Repeat("█", filled)), sDim.Render(strings.Repeat("░", width-filled)), m.prog.i, total, m.prog.title)
		b.WriteString(sDim.Render("\nЖурнал: " + logPath))

	case scDone:
		if m.runErr != nil {
			b.WriteString(sBad.Render("Установка не удалась") + "\n\n" + m.runErr.Error() + "\n\n")
			b.WriteString(sDim.Render("Подробности в " + logPath + ". Enter — выход в консоль."))
		} else {
			b.WriteString(sGood.Render("Готово.") + "\n\n")
			b.WriteString("Извлеките установочный носитель и нажмите Enter для перезагрузки.\n\n")
			b.WriteString("После загрузки веб-интерфейс будет на адресе управления, порт 443.\nВход: admin / admin.\n")
		}
	}
	if m.err != "" {
		b.WriteString("\n\n" + sBad.Render(m.err))
	}
	return lipgloss.NewStyle().Padding(1, 3).Render(b.String())
}
