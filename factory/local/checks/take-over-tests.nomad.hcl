# A raw_exec task that runs the wrapper's take-over cases (factory/wrapper) as root, as
# the wrapper runs on an agent VM, with the run user agent (cluster check 6). One case
# uses the real hc of the image (FACTORYTEST_HC): the wrapper starts the hc core as
# agent in the take-over Execution's cgroup, with no SUDO_ variable, and the user
# engineer (the access group) attaches to it with 'sudo -u agent hc connect'. The
# other cases use the fake hc. KILLBOUNDARY_REQUIRED makes a case that skips fail.
# The smoke script copies the test binary (static) to the agent machine, runs this job
# and reads its output.
job "take-over-tests" {
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
        command = "/opt/factory-checks/wrapper.test"
        args = [
          "-test.v", "-test.count=1", "-test.timeout=4m",
          "-test.run", "TakeOver|TakenOver",
        ]
      }

      env {
        KILLBOUNDARY_REQUIRED = "1"
        FACTORYTEST_RUN_USER  = "agent"
        FACTORYTEST_HC        = "/usr/local/bin/hc"
        TMPDIR                = "/tmp"
      }

      resources {
        cpu    = 500
        memory = 512
      }
    }
  }
}
