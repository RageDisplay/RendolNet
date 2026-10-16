package api

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// hostSampler читает загрузку узла из /proc. Под с hostNetwork видит
// /proc/stat и /proc/meminfo всего узла, а не только своего контейнера.
type hostSampler struct {
	mu       sync.Mutex
	prevIdle uint64
	prevAll  uint64
	cpu      float64
}

func newHostSampler() *hostSampler { return &hostSampler{} }

func (h *hostSampler) sample() {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return
	}
	var all, idle uint64
	for i, v := range fields[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		all += n
		if i == 3 || i == 4 {
			idle += n
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.prevAll > 0 && all > h.prevAll {
		h.cpu = 100 * (1 - float64(idle-h.prevIdle)/float64(all-h.prevAll))
	}
	h.prevAll, h.prevIdle = all, idle
}

type HostInfo struct {
	CPUPercent float64    `json:"cpu_percent"`
	CPUs       int        `json:"cpus"`
	MemTotal   uint64     `json:"mem_total"`
	MemUsed    uint64     `json:"mem_used"`
	Load       [3]float64 `json:"load"`
	Uptime     float64    `json:"uptime_s"`
	DiskTotal  uint64     `json:"disk_total"`
	DiskUsed   uint64     `json:"disk_used"`
	Kernel     string     `json:"kernel"`
	Hostname   string     `json:"hostname"`
	Time       time.Time  `json:"time"`
}

func (h *hostSampler) snapshot(dataDir string) HostInfo {
	h.mu.Lock()
	info := HostInfo{CPUPercent: h.cpu, CPUs: runtime.NumCPU(), Time: time.Now()}
	h.mu.Unlock()

	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		vals := map[string]uint64{}
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
			vals[k] = n * 1024
		}
		info.MemTotal = vals["MemTotal"]
		info.MemUsed = vals["MemTotal"] - vals["MemAvailable"]
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		for i := 0; i < 3 && i < len(f); i++ {
			info.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			info.Uptime, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		info.Kernel = strings.TrimSpace(string(b))
	}
	info.Hostname, _ = os.Hostname()
	if dataDir != "" {
		info.DiskTotal, info.DiskUsed, _ = diskUsage(dataDir)
	}
	return info
}
