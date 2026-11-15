# A raw_exec task that runs the cgroup cases of the wrapper's kill boundary
# (factory/internal/killboundary) as root, as the wrapper runs on an agent VM. The
# process trees of the cases run as the run user. KILLBOUNDARY_REQUIRED makes a case
# that skips fail, so the run proves each case here.
# The smoke script builds the test binary, copies it to the agent machine, runs this
# job and reads its output.
job "kill-boundary-tests" {
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

    task "tests" {
      driver = "raw_exec"

      config {
        command = "/opt/factory-checks/killboundary.test"
        args    = ["-test.v", "-test.count=1"]
      }

      env {
        KILLBOUNDARY_REQUIRED = "1"
        KILLBOUNDARY_RUN_USER = "agent"
        TMPDIR                = "/tmp"
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }
  }
}
