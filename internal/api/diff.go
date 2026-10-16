package api

import (
	"encoding/json"
	"strings"
)

type DiffLine struct {
	Op   string `json:"op"` // " ", "+", "-"
	Text string `json:"text"`
}

// configDiff — построчная разница двух конфигов в виде отформатированного
// JSON. Для конфигов в тысячи строк LCS за O(n·m) достаточно быстр.
func configDiff(a, b any) []DiffLine {
	al := jsonLines(a)
	bl := jsonLines(b)
	n, m := len(al), len(bl)

	// общий префикс и суффикс снимаем заранее — обычно правка маленькая
	pre := 0
	for pre < n && pre < m && al[pre] == bl[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && al[n-1-suf] == bl[m-1-suf] {
		suf++
	}
	x, y := al[pre:n-suf], bl[pre:m-suf]

	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var out []DiffLine
	for _, l := range al[:pre] {
		out = append(out, DiffLine{" ", l})
	}
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		switch {
		case x[i] == y[j]:
			out = append(out, DiffLine{" ", x[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{"-", x[i]})
			i++
		default:
			out = append(out, DiffLine{"+", y[j]})
			j++
		}
	}
	for ; i < len(x); i++ {
		out = append(out, DiffLine{"-", x[i]})
	}
	for ; j < len(y); j++ {
		out = append(out, DiffLine{"+", y[j]})
	}
	for _, l := range al[n-suf:] {
		out = append(out, DiffLine{" ", l})
	}
	return out
}

func jsonLines(v any) []string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return strings.Split(string(b), "\n")
}

func diffChanged(d []DiffLine) bool {
	for _, l := range d {
		if l.Op != " " {
			return true
		}
	}
	return false
}
