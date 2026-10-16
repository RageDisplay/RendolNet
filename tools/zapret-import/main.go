// zapret-import <каталог-релиза-Flowseal> <strategies.json>
// Переводит все general*.bat в список стратегий для nfqws.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rendolnet/internal/zapret"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "zapret-import <каталог> <strategies.json>")
		os.Exit(2)
	}
	files, _ := filepath.Glob(filepath.Join(os.Args[1], "general*.bat"))
	sort.Strings(files)
	var out []zapret.Strategy
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		name := strings.TrimSuffix(filepath.Base(f), ".bat")
		s := zapret.Convert(name, string(raw))
		mark := "ok"
		if !s.Compatible {
			mark = "НЕ ПЕРЕНОСИТСЯ: " + strings.Join(s.Problems, "; ")
		}
		fmt.Printf("%-32s %s\n", name, mark)
		out = append(out, s)
	}
	if len(out) == 0 {
		fmt.Fprintln(os.Stderr, "в каталоге нет general*.bat")
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(os.Args[2], append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
