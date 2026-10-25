package supervisor

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func sampleProcess(p *process) (memory, cpu *float64) {
	base := "/proc/" + strconv.Itoa(p.cmd.Process.Pid) + "/"
	if b, err := os.ReadFile(base + "status"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "VmRSS:" {
				if kb, err := strconv.ParseFloat(fields[1], 64); err == nil {
					mb := kb / 1024
					memory = &mb
				}
			}
		}
	}
	// Linux exposes process-wide user/system CPU in USER_HZ (100 ticks/s).
	if b, err := os.ReadFile(base + "stat"); err == nil {
		end := strings.LastIndexByte(string(b), ')')
		if end >= 0 {
			fields := strings.Fields(string(b)[end+1:])
			if len(fields) > 12 {
				user, e1 := strconv.ParseInt(fields[11], 10, 64)
				system, e2 := strconv.ParseInt(fields[12], 10, 64)
				if e1 == nil && e2 == nil {
					nanos := (user + system) * int64(time.Second) / 100
					now := time.Now()
					if !p.sampled.IsZero() && nanos >= p.cpuNanos {
						value := float64(nanos-p.cpuNanos) / float64(now.Sub(p.sampled)) * 100
						cpu = &value
					}
					p.sampled = now
					p.cpuNanos = nanos
				}
			}
		}
	}
	return
}
