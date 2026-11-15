# A raw_exec task with the wrapper's workload identity in a file, as the Nomad Runner
# gives it. As root it reads the JWT; as the run user it cannot, and it cannot reach
# the task's directory (the wrapper's own files), and the task's environment has no
# token. The task prints PASS or the first problem.
job "identity-file-check" {
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

      identity {
        name = "dispatcher"
        aud  = ["solidtrace-dispatcher"]
        file = true
        ttl  = "1h"
      }

      config {
        command = "/bin/sh"
        args = ["-c", <<-EOT
          fail() { echo "FAIL: $*"; exit 1; }
          jwt="$NOMAD_SECRETS_DIR/nomad_dispatcher.jwt"
          [ "$(id -u)" = 0 ] || fail "the task does not run as root"
          [ -s "$jwt" ] || fail "no identity file at $jwt"
          as_agent() { setpriv --reuid agent --regid agent --init-groups -- "$@"; }
          ! as_agent cat "$jwt" >/dev/null 2>&1 || fail "the run user reads the identity file"
          ! as_agent ls "$NOMAD_TASK_DIR" >/dev/null 2>&1 || fail "the run user reaches the task's directory"
          ! as_agent ls "$(pwd)" >/dev/null 2>&1 || fail "the run user reaches the task's working directory"
          ! env | grep -q '^NOMAD_TOKEN' || fail "a token in the task's environment"
          echo "PASS: the identity file is root's only; the run user reaches no file of the task"
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
