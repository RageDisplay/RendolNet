package zapret

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"rendolnet/internal/netcfg"
)

// CheckAll прогоняет каждую стратегию через nfqws --dry-run. Вызывается при
// сборке образа: если nfqws не понимает какой-то аргумент, образ не соберётся.
func CheckAll(nfqws, baseDir string) error {
	work, err := os.MkdirTemp("", "zapret-check")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	lists, err := PrepareLists(baseDir, work, netcfg.Zapret{})
	if err != nil {
		return err
	}
	var bad []string
	for _, s := range Strategies() {
		if !s.Compatible {
			continue
		}
		args := append([]string{"--dry-run", fmt.Sprintf("--qnum=%d", QueueNum)},
			s.Expand(filepath.Join(baseDir, "bin"), lists)...)
		out, err := exec.CommandContext(context.Background(), nfqws, args...).CombinedOutput()
		// --dry-run после разбора параметров пробует сбросить привилегии, а в
		// песочнице docker build это запрещено. Параметры к этому моменту уже
		// проверены — профили посчитаны.
		if err != nil && strings.HasPrefix(lastLine(string(out)), "setpcap:") && strings.Contains(string(out), "desync profile") {
			err = nil
		}
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v: %s", s.Name, err, lastLine(string(out))))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("nfqws не принял стратегии:\n%s", strings.Join(bad, "\n"))
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
