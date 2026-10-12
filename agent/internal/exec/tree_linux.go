//go:build linux

package exec

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func ProcessAlive(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(data)
	end := strings.LastIndex(s, ")")
	if end < 0 || end+2 >= len(s) {
		return true
	}
	fields := strings.Fields(s[end+2:])
	return len(fields) == 0 || fields[0] != "Z"
}

func KillTree(rootPID int) { killTree(rootPID) }

func killTree(rootPID int) {
	syscall.Kill(rootPID, syscall.SIGSTOP)

	descendants := snapshotDescendants(rootPID)
	for _, pid := range descendants {
		syscall.Kill(pid, syscall.SIGSTOP)
	}
	descendants = snapshotDescendants(rootPID)

	for _, pid := range descendants {
		syscall.Kill(pid, syscall.SIGKILL)
	}
	syscall.Kill(-rootPID, syscall.SIGKILL)
	syscall.Kill(rootPID, syscall.SIGKILL)
	for _, pid := range descendants {
		syscall.Kill(pid, syscall.SIGCONT)
	}
	syscall.Kill(rootPID, syscall.SIGCONT)
}

func snapshotDescendants(root int) []int {
	ppidOf := map[int]int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if ppid, ok := readPPID(pid); ok {
			ppidOf[pid] = ppid
		}
	}

	var out []int
	queue := []int{root}
	seen := map[int]bool{root: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for pid, ppid := range ppidOf {
			if ppid == cur && !seen[pid] {
				seen[pid] = true
				out = append(out, pid)
				queue = append(queue, pid)
			}
		}
	}
	return out
}

func readPPID(pid int) (int, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	s := string(data)
	end := strings.LastIndex(s, ")")
	if end < 0 || end+2 >= len(s) {
		return 0, false
	}
	fields := strings.Fields(s[end+2:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}
