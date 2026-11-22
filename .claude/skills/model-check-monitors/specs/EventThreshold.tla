--------------------------- MODULE EventThreshold ---------------------------
(***************************************************************************)
(* The event threshold check of one Issue for one Integration:            *)
(* EventThresholdJob -> IntegrationNotification.check_event_threshold      *)
(* (console/app/jobs/event_threshold_job.rb,                               *)
(* console/app/services/integration_notification.rb).                      *)
(*                                                                         *)
(* Each Event that the Issue receives enqueues one EventThresholdJob       *)
(* (IntegrationNotificationJob). The job has limits_concurrency to: 1,     *)
(* key: the Issue, duration: 2.minutes, and the default on_conflict:       *)
(* :block. Solid Queue 1.3.1 takes the semaphore when the job is           *)
(* dispatched, gives it back when the job ends, and deletes it 2 minutes   *)
(* after it was taken, also while the job waits or runs. A blocked job     *)
(* gets it when a job ends (unblock_next_blocked_job) or at concurrency    *)
(* maintenance (BlockedExecution.unblock).                                 *)
(*                                                                         *)
(* Steps, each one statement or one call:                                  *)
(*   Dispatch  the semaphore, or the job waits blocked                     *)
(*   Check     threshold_reached_since?: Notification.exists?              *)
(*   Count     issue.events.newer_than(window_start).count! (EventStore);  *)
(*             EventStore::Unavailable -> retry_on enqueues the job again  *)
(*   Insert    Notification.enqueue! of threshold_reached, then the end    *)
(* The model has the Events in the window over the threshold, so each     *)
(* count passes it. Left out: other Integrations, the tick.               *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
  Jobs,           \* one EventThresholdJob for each Event that arrives
  CountCanFail,   \* TRUE: EventStore can be unavailable for a count
  LockCanExpire   \* TRUE: a job can wait or run longer than 2 minutes

VARIABLES
  jpc,     \* the step of each job
  tries,   \* the retries of each job
  sem,     \* the Issue's semaphore: "none", "taken" or "free"
  rows     \* threshold_reached rows for the Issue and Integration in the window

vars == <<jpc, tries, sem, rows>>

Acquirable == sem \in {"none", "free"}
Signal == IF sem = "taken" THEN "free" ELSE sem

Init ==
  /\ jpc = [j \in Jobs |-> "enqueued"]
  /\ tries = [j \in Jobs |-> 0]
  /\ sem = "none"
  /\ rows = 0

\* The job is dispatched: Semaphore#wait, or a BlockedExecution.
Dispatch(j) ==
  /\ jpc[j] = "enqueued"
  /\ IF Acquirable
       THEN /\ sem' = "taken"
            /\ jpc' = [jpc EXCEPT ![j] = "ready"]
       ELSE /\ jpc' = [jpc EXCEPT ![j] = "blocked"]
            /\ UNCHANGED sem
  /\ UNCHANGED <<tries, rows>>

\* A blocked job gets the semaphore.
Unblock(j) ==
  /\ jpc[j] = "blocked"
  /\ Acquirable
  /\ sem' = "taken"
  /\ jpc' = [jpc EXCEPT ![j] = "ready"]
  /\ UNCHANGED <<tries, rows>>

\* event_threshold_reached?: a threshold_reached row in the window ends the
\* check, and the job ends.
Check(j) ==
  /\ jpc[j] = "ready"
  /\ IF rows > 0
       THEN /\ jpc' = [jpc EXCEPT ![j] = "done"]
            /\ sem' = Signal
       ELSE /\ jpc' = [jpc EXCEPT ![j] = "counting"]
            /\ UNCHANGED sem
  /\ UNCHANGED <<tries, rows>>

\* The windowed count from EventStore. When EventStore is unavailable,
\* retry_on enqueues the job again with a wait, and this run of it ends.
Count(j) ==
  /\ jpc[j] = "counting"
  /\ \/ /\ jpc' = [jpc EXCEPT ![j] = "insert"]
        /\ UNCHANGED <<tries, sem>>
     \/ /\ CountCanFail
        /\ tries[j] < 1
        /\ tries' = [tries EXCEPT ![j] = tries[j] + 1]
        /\ jpc' = [jpc EXCEPT ![j] = "enqueued"]
        /\ sem' = Signal
  /\ UNCHANGED rows

\* record: Notification.enqueue! and schedule_tick; then the job ends.
Insert(j) ==
  /\ jpc[j] = "insert"
  /\ rows' = rows + 1
  /\ jpc' = [jpc EXCEPT ![j] = "done"]
  /\ sem' = Signal
  /\ UNCHANGED tries

\* Concurrency maintenance deletes the semaphore 2 minutes after it was taken.
Expire ==
  /\ LockCanExpire
  /\ sem = "taken"
  /\ sem' = "none"
  /\ UNCHANGED <<jpc, tries, rows>>

Done ==
  /\ \A j \in Jobs : jpc[j] = "done"
  /\ UNCHANGED vars

Next ==
  \/ Expire
  \/ \E j \in Jobs : Dispatch(j) \/ Unblock(j) \/ Check(j) \/ Count(j) \/ Insert(j)
  \/ Done

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ jpc \in [Jobs -> {"enqueued", "blocked", "ready", "counting", "insert", "done"}]
  /\ sem \in {"none", "taken", "free"}

-----------------------------------------------------------------------------
(* The rule. *)

\* The Integration gets one threshold reached Notification for the Issue in
\* a window, not one for each Event that arrived together.
OneThresholdRow == rows <= 1
=============================================================================
