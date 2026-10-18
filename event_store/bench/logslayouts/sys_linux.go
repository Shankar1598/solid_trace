package logslayouts

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// rssBytes is this process's resident set size.
func rssBytes() int64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if rest, ok := strings.CutPrefix(s.Text(), "VmRSS:"); ok {
			kb, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			return kb * 1024
		}
	}
	return 0
}

// cpuSeconds is this process's user plus system CPU time.
func cpuSeconds() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return float64(ru.Utime.Nano()+ru.Stime.Nano()) / 1e9
}

// dropPageCache asks the kernel to evict every file under dir from the page
// cache, so the next read is cold without needing root.
func dropPageCache(dir string) {
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return nil
		}
		defer f.Close()
		f.Sync()
		const posixFadvDontneed = 4
		syscall.Syscall6(syscall.SYS_FADVISE64, f.Fd(), 0, 0, posixFadvDontneed, 0, 0)
		return nil
	})
}

// dirBytes is the total size of the files under dir.
func dirBytes(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
