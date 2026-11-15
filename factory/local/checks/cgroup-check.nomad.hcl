# A raw_exec task that does what the wrapper does at the kill boundary (cluster check 13):
# as root, it makes a child cgroup (cgroup v2), starts a process in it as the run user
# (a shell with a child, and a process in a session of its own), and stops them all
# with cgroup.kill. It also checks that the task has no NET_ADMIN.
# The smoke script runs it and reads its output.
job "cgroup-check" {
  type = "batch"

  group "check" {
    restart {
      attempts = 0
      mode     = "fail"
    }

    reschedule {
      attempts  = 0
      unlimited = false
    }

    task "check" {
      driver = "raw_exec"

      config {
        command = "/bin/bash"
        args = ["-c", <<-EOT
          set -euo pipefail
          fail() { echo "FAIL: $*"; exit 1; }

          [ "$(id -u)" = 0 ] || fail "the task does not run as root"
          caps=$(awk '/^CapEff/ { print $2 }' /proc/self/status)
          (( (0x$caps >> 12) & 1 )) && fail "the task has NET_ADMIN"

          own=$(sed -n 's/^0:://p' /proc/self/cgroup)
          child="/sys/fs/cgroup$own/execution"
          mkdir "$child"
          [ -e "$child/cgroup.kill" ] || fail "no cgroup.kill in $child"

          # The child shell joins the cgroup, then changes to the run user.
          bash -c 'echo $$ >"$1/cgroup.procs" && exec setpriv --reuid agent --regid agent --init-groups -- bash -c "sleep 300 & setsid sleep 300 & wait"' _ "$child" &
          for _ in $(seq 1 50); do
            [ "$(wc -l <"$child/cgroup.procs")" -ge 3 ] && break
            sleep 0.1
          done
          count=$(wc -l <"$child/cgroup.procs")
          [ "$count" -ge 3 ] || fail "$count processes in the child cgroup, want 3"
          sessions=$(for pid in $(cat "$child/cgroup.procs"); do ps -o sid= -p "$pid"; done | sort -u | wc -l)
          [ "$sessions" -ge 2 ] || fail "no process in a session of its own"
          for pid in $(cat "$child/cgroup.procs"); do
            [ "$(stat -c %U /proc/$pid)" = agent ] || fail "process $pid does not run as the run user"
          done

          echo 1 >"$child/cgroup.kill"
          for _ in $(seq 1 50); do
            grep -q '^populated 0' "$child/cgroup.events" && break
            sleep 0.1
          done
          grep -q '^populated 0' "$child/cgroup.events" || fail "the child cgroup is not empty after cgroup.kill"
          rmdir "$child"
          echo "PASS: $count processes of the run user in $sessions sessions in a child cgroup; cgroup.kill stopped them all"
        EOT
        ]
      }

      resources {
        cpu    = 100
        memory = 64
      }
    }
  }
}
