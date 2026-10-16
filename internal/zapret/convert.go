package zapret

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Пути в аргументах стратегии. Подставляются при запуске nfqws.
const (
	BinDir   = "{BIN}"
	ListsDir = "{LISTS}"
)

// Опции winws, которых нет у nfqws. --wf-* переводятся в порты для nftables,
// остальные делают стратегию непереносимой.
var winOnly = []string{"--wf-raw", "--wf-l3", "--wf-save", "--ssid-filter", "--nlm-filter", "--wf-raw-part", "--wf-tcp-in", "--wf-udp-in"}

var startRe = regexp.MustCompile(`(?i)winws\.exe"?\s+`)

// Convert разбирает .bat из сборки Flowseal: аргументы winws, порты фильтра
// WinDivert, профили через --new.
func Convert(name, bat string) Strategy {
	s := Strategy{Name: name, Source: name + ".bat", Compatible: true}

	text := strings.ReplaceAll(bat, "\r", "")
	text = strings.ReplaceAll(text, "^\n", " ")
	var line string
	for _, l := range strings.Split(text, "\n") {
		if startRe.MatchString(l) {
			line = l
			break
		}
	}
	if line == "" {
		s.Compatible = false
		s.Problems = append(s.Problems, "в файле нет запуска winws.exe")
		return s
	}
	line = line[startRe.FindStringIndex(line)[1]:]
	tokens := split(line)

	var profiles [][]string
	cur := []string{}
	for _, tok := range tokens {
		tok = strings.ReplaceAll(tok, "%BIN%", BinDir+"/")
		tok = strings.ReplaceAll(tok, "%LISTS%", ListsDir+"/")
		key, val, _ := strings.Cut(tok, "=")
		switch {
		case key == "--wf-tcp":
			s.TCPPorts = mergePorts(s.TCPPorts, val)
			continue
		case key == "--wf-udp":
			s.UDPPorts = mergePorts(s.UDPPorts, val)
			continue
		case tok == "--new":
			profiles = append(profiles, cur)
			cur = []string{}
			continue
		}
		for _, w := range winOnly {
			if key == w {
				s.Compatible = false
				s.Problems = append(s.Problems, "опция "+key+" есть только в Windows")
			}
		}
		cur = append(cur, tok)
	}
	profiles = append(profiles, cur)

	// Игровой фильтр в сборке выключен по умолчанию: такие профили не берём.
	var kept [][]string
	for _, p := range profiles {
		if len(p) == 0 {
			continue
		}
		game := false
		for _, a := range p {
			if strings.Contains(a, "%GameFilter") {
				game = true
			}
		}
		if !game {
			kept = append(kept, p)
		}
	}
	for i, p := range kept {
		if i > 0 {
			s.Args = append(s.Args, "--new")
		}
		s.Args = append(s.Args, p...)
	}
	for _, a := range s.Args {
		if strings.Contains(a, "%") {
			s.Compatible = false
			s.Problems = append(s.Problems, "неизвестная переменная в "+a)
		}
	}
	if len(s.TCPPorts) == 0 && len(s.UDPPorts) == 0 {
		s.Compatible = false
		s.Problems = append(s.Problems, "не найдены порты --wf-tcp/--wf-udp")
	}
	return s
}

// split делит командную строку cmd.exe: пробелы, двойные кавычки, ^ вне
// кавычек экранирует следующий символ (^! → !).
func split(s string) []string {
	var out []string
	var b strings.Builder
	inQ, has, esc := false, false, false
	for _, r := range s {
		switch {
		case esc:
			b.WriteRune(r)
			has, esc = true, false
		case r == '^' && !inQ:
			esc = true
		case r == '"':
			inQ = !inQ
			has = true
		case (r == ' ' || r == '\t') && !inQ:
			if has {
				out = append(out, b.String())
				b.Reset()
				has = false
			}
		default:
			b.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, b.String())
	}
	return out
}

func mergePorts(have []string, list string) []string {
	seen := map[string]bool{}
	for _, p := range have {
		seen[p] = true
	}
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" || strings.Contains(p, "%") || seen[p] {
			continue
		}
		seen[p] = true
		have = append(have, p)
	}
	sort.Slice(have, func(i, j int) bool { return portStart(have[i]) < portStart(have[j]) })
	return have
}

func portStart(p string) int {
	lo, _, _ := strings.Cut(p, "-")
	n, _ := strconv.Atoi(lo)
	return n
}

// Expand подставляет каталоги в аргументы стратегии.
func (s Strategy) Expand(bin, lists string) []string {
	out := make([]string, len(s.Args))
	for i, a := range s.Args {
		a = strings.ReplaceAll(a, BinDir, bin)
		out[i] = strings.ReplaceAll(a, ListsDir, lists)
	}
	return out
}

func (s Strategy) String() string {
	return fmt.Sprintf("%s (tcp %s, udp %s)", s.Name, strings.Join(s.TCPPorts, ","), strings.Join(s.UDPPorts, ","))
}
