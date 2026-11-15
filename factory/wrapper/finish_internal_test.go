package wrapper

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeBoundary is a kill boundary whose cgroup is empty only after
// emptyAfter kills.
type fakeBoundary struct {
	emptyAfter int
	kills      int
	removes    int
}

func (b *fakeBoundary) Start(*exec.Cmd) error    { return nil }
func (b *fakeBoundary) Populated() (bool, error) { return b.kills < b.emptyAfter, nil }
func (b *fakeBoundary) Kill() error              { b.kills++; return nil }
func (b *fakeBoundary) Remove(time.Duration) error {
	b.removes++
	if b.kills < b.emptyAfter {
		return fmt.Errorf("remove the child cgroup: %w", syscall.EBUSY)
	}
	return nil
}

// A process can join the cgroup after the first cgroup.kill. The end
// procedure then kills again, once, before it says that processes are
// left.
func TestTheEndProcedureKillsAgainWhenTheCgroupIsNotEmptyAfterTheFirstKill(t *testing.T) {
	cases := []struct {
		name       string
		emptyAfter int
		kills      int
		left       bool
	}{
		{"empty after the first kill", 1, 1, false},
		{"empty after the second kill", 2, 2, false},
		{"not empty after the second kill", 3, 2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &fakeBoundary{emptyAfter: c.emptyAfter}
			var log bytes.Buffer
			e := &execution{cfg: config{docker: "/no/docker"}, log: &log, boundary: b}

			e.finish()

			if b.kills != c.kills {
				t.Errorf("cgroup.kill %d times, want %d", b.kills, c.kills)
			}
			if (e.left != nil) != c.left {
				t.Errorf("left %v, want processes left: %v", e.left, c.left)
			}
			if said := strings.Contains(log.String(), "the end procedure left processes"); said != c.left {
				t.Errorf("the wrapper's output:\n%s", log.String())
			}
		})
	}
}
