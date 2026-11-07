package agentd

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Headroom is the host's load and memory a claim reports (R5 phase 2 step
// 3b): the one-minute load, the cpus, and the memory available and in
// total, in bytes. A field that cannot be read is nil and left out of the
// claim; the platform reads them as slot signals (store.SlotSignals).
type Headroom struct {
	Load1    *float64
	CPUs     int
	MemAvail *int64
	MemTotal *int64
}

// Overloaded says whether the one-minute load is above the number of cores
// (lesson 105): the host asks for nothing until it eases. A load not read is
// not overloaded.
func (h Headroom) Overloaded() bool {
	return h.Load1 != nil && *h.Load1 > float64(h.CPUs)
}

// MeasureHeadroom reads the host's headroom: the load from /proc/loadavg or,
// where there is none (macOS), from sysctl vm.loadavg; the memory from
// /proc/meminfo, and on a host without it none. sysctl runs the sysctl
// command (nil: not tried).
func MeasureHeadroom(sysctl func(args ...string) ([]byte, error)) Headroom {
	h := Headroom{CPUs: runtime.NumCPU()}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		h.Load1 = ParseLoad(string(b))
	} else if sysctl != nil {
		if b, err := sysctl("-n", "vm.loadavg"); err == nil {
			h.Load1 = ParseLoad(string(b))
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		h.MemAvail, h.MemTotal = ParseMeminfo(string(b))
	}
	return h
}

// ParseLoad reads the one-minute load from /proc/loadavg ("0.52 0.58 0.59
// 1/123 4567") or sysctl vm.loadavg ("{ 0.52 0.58 0.59 }"); nil when it is
// not one.
func ParseLoad(s string) *float64 {
	fields := strings.Fields(strings.Trim(strings.TrimSpace(s), "{} "))
	if len(fields) == 0 {
		return nil
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || load < 0 {
		return nil
	}
	return &load
}

// ParseMeminfo reads MemAvailable and MemTotal (kB) from /proc/meminfo, in
// bytes; both nil unless both are there and available is at most the total.
func ParseMeminfo(s string) (avail, total *int64) {
	var a, t int64 = -1, -1
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok || (name != "MemAvailable" && name != "MemTotal") {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 || len(f) > 1 && f[1] != "kB" {
			continue
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil || n < 0 {
			continue
		}
		if name == "MemAvailable" {
			a = n << 10
		} else {
			t = n << 10
		}
	}
	if a < 0 || t <= 0 || a > t {
		return nil, nil
	}
	return &a, &t
}
