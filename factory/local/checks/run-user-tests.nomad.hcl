# A raw_exec task that runs the wrapper's run-user cases (factory/wrapper) as root, as
# the wrapper runs on an agent VM. Each case starts wrappers that change to the run
# user agent for the harness, its tools and git. KILLBOUNDARY_REQUIRED makes a case
# that skips fail, so the run proves each case here.
# The smoke script builds the test binary (static: the Dispatcher in the tests uses
# SQLite through cgo), copies it to the agent machine, runs this job and reads its
# output.
job "run-user-tests" {
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
        # The skipped case is for a wrapper that is not root: it skips as root.
        args = [
          "-test.v", "-test.count=1", "-test.timeout=4m",
          "-test.run", "RunUser|TheExecutionsDirectory",
          "-test.skip", "TestTheWrapperRefusesARunUserThatItCannotChangeTo",
        ]
      }

      env {
        KILLBOUNDARY_REQUIRED = "1"
        FACTORYTEST_RUN_USER  = "agent"
        TMPDIR                = "/tmp"
      }

      resources {
        cpu    = 500
        memory = 512
      }
    }
  }
}
