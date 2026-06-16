package agent

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type sysInfo struct {
	hostname string
	localIP  string
	cpu      string
	memMB    int64
	diskGB   int64
}

func collectSysInfo() sysInfo {
	hostname, _ := os.Hostname()
	return sysInfo{
		hostname: hostname,
		localIP:  detectPrivateIP(),
		cpu:      cpuInfo(),
		memMB:    memTotalMB(),
		diskGB:   diskTotalGB("/"),
	}
}

func cpuInfo() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "0"
	}
	defer f.Close()

	cores := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "processor") {
			cores++
		}
	}
	return strconv.Itoa(cores)
}

func memTotalMB() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return kb / 1024
			}
		}
	}
	return 0
}

func diskTotalGB(path string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	return int64(stat.Blocks) * int64(stat.Bsize) / (1024 * 1024 * 1024)
}
