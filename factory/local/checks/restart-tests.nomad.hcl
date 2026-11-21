# A raw_exec task that runs the wrapper's restore and restart cases (factory/wrapper,
# ticket 66) as root, as the wrapper runs on an agent VM, with the run user agent: the
# restore's git runs as agent in the new checkout, and the save as agent too.
# KILLBOUNDARY_REQUIRED makes a case that skips fail. The smoke script copies the test
# binary (static) to the agent machine, runs this job and reads its output.
job "restart-tests" {
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
          "-test.run", "Restart|Restore|PersonsPush|LostMachine|TheFourthLost|EndsForAnotherReason|WaitForAMachine|PreemptionDuringATakeOver",
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
