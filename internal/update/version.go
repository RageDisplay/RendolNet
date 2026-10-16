package update

import (
	"strconv"
	"strings"
	"unicode"
)

// CompareVersions сравнивает версии так же, как sort -V: числа — как числа,
// остальное — как строки. "1.2.10" > "1.2.9", "1.2.0-5-gabc" > "1.2.0".
func CompareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	ta, tb := tokens(a), tokens(b)
	for i := 0; i < len(ta) && i < len(tb); i++ {
		x, y := ta[i], tb[i]
		xn, xerr := strconv.ParseUint(x, 10, 64)
		yn, yerr := strconv.ParseUint(y, 10, 64)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
		case x != y:
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ta) < len(tb):
		return -1
	case len(ta) > len(tb):
		return 1
	}
	return 0
}

func tokens(s string) []string {
	var out []string
	var cur strings.Builder
	digit := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if r == '.' || r == '-' || r == '+' {
			flush()
			continue
		}
		d := unicode.IsDigit(r)
		if cur.Len() > 0 && d != digit {
			flush()
		}
		digit = d
		cur.WriteRune(r)
	}
	flush()
	return out
}
