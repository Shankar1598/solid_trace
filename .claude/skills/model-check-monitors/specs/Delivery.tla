------------------------------ MODULE Delivery ------------------------------
(***************************************************************************)
(* Delivery of one Integration's Monitor Notifications: the outbox rows   *)
(* (console/app/models/notification.rb) and the per-Integration tick       *)
(* (IntegrationNotificationProcessorJob -> IntegrationNotification          *)
(* .deliver_pending, console/app/services/integration_notification.rb).    *)
(*                                                                         *)
(* The rows are one Monitor group's chain: a firing row, then the resolved *)
(* row that closes it. A Monitor run writes each row in its transaction    *)
(* (monitor_changes) and then schedules a tick (schedule_ticks).           *)
(*                                                                         *)
(* The tick has limits_concurrency to: 1, duration: 2.minutes,             *)
(* on_conflict: :discard. Solid Queue 1.3.1 (SolidQueue::Semaphore) takes   *)
(* the semaphore when the job is dispatched, gives it back when the job    *)
(* finishes or its process dies, and its concurrency maintenance deletes a *)
(* semaphore 2 minutes after it was taken, also while the job runs.        *)
(*                                                                         *)
(* With StatusChain, the rows are the Monitor status chain instead: a      *)
(* "not running", then the "running again" that follows it. A running      *)
(* again whose not running ended with nothing sent is skipped              *)
(* (never_told?), as the Integration was never told.                       *)
(*                                                                         *)
(* Steps, each one statement or one call:                                  *)
(*   Dispatch  the tick's job is dispatched: the semaphore, or discard     *)
(*   Pluck     Notification.pending_for(...).pluck(:id)                    *)
(*   Mark      Notification.mark_rows([row.id], :processing)               *)
(*   Send      the provider call, then mark_rows :sent or :failed          *)
(*   Follow    the deliverable.exists? check, the follow-up tick, and the  *)
(*             end of the job. They are one step here: nothing slow runs   *)
(*             between them, and a new row's tick waits INITIAL_DELAY.     *)
(* Left out: Issue rows, the rate limit window (it only delays a tick),    *)
(* a missing or inactive Integration, and NotConfigured (skipped).         *)
(* MaxTries stands for MONITOR_RETRY_FOR (6 hours of failed tries).        *)
(***************************************************************************)
EXTENDS Naturals, Sequences

CONSTANTS
  MaxTicks,       \* the ticks that the model makes
  MaxTries,       \* tries of a failed row before its outcome is final
  SendCanFail,    \* TRUE: the provider can answer an error
  LockCanExpire,  \* TRUE: a tick can wait or run longer than 2 minutes
  TicksCanDie,    \* TRUE: a tick's process can die (out of memory, a kill, a deploy)
  WritersCanDie,  \* TRUE: a Monitor run's process can die after its commit
  StatusChain     \* TRUE: the Monitor status chain, with never_told?

Rows == 1..2      \* 1: monitor_firing, 2: the monitor_resolved that closes it
Ticks == 1..MaxTicks

VARIABLES
  written,    \* rows written so far
  writer,     \* "idle", or "scheduling": the run committed a row, schedule_ticks is next
  status,     \* Notification#status of each row
  attempts,   \* Notification#attempts
  sends,      \* sends[i]: messages that the provider got for row i
  early,      \* a resolve went while the trigger it closes could still go
  alone,      \* a resolve went after the trigger it closes was given up
  tpc,        \* the step of each tick
  queue,      \* the rows a tick still delivers, oldest first
  sem,        \* the Integration's semaphore: "none" (no row), "taken" (value 0) or "free" (value 1)
  budgetHit   \* the model needed more than MaxTicks ticks

vars == <<written, writer, status, attempts, sends, early, alone, alone, tpc, queue, sem, budgetHit>>

Deliverable(i) ==
  /\ i <= written
  /\ \/ status[i] = "pending"
     \/ status[i] = "failed" /\ attempts[i] < MaxTries

\* Failed for the last time: nothing was sent, and nothing will be.
GivenUp(i) == status[i] = "failed" /\ attempts[i] >= MaxTries

Settled(i) ==
  \/ status[i] \in {"sent", "skipped"}
  \/ status[i] = "failed" /\ attempts[i] >= MaxTries

Init ==
  /\ written = 0
  /\ writer = "idle"
  /\ status = [i \in Rows |-> "pending"]
  /\ attempts = [i \in Rows |-> 0]
  /\ sends = [i \in Rows |-> 0]
  /\ early = FALSE
  /\ alone = FALSE
  /\ tpc = [t \in Ticks |-> "unused"]
  /\ queue = [t \in Ticks |-> <<>>]
  /\ sem = "none"
  /\ budgetHit = FALSE

\* schedule_tick: IntegrationNotificationProcessorJob.set(wait: ...).perform_later.
\* A new tick in tpc', from tpc0, the ticks as this step leaves them otherwise.
Schedule(tpc0) ==
  IF \E t \in Ticks : tpc0[t] = "unused"
    THEN LET t == CHOOSE t \in Ticks : tpc0[t] = "unused" /\ \A u \in Ticks : tpc0[u] = "unused" => t <= u
         IN /\ tpc' = [tpc0 EXCEPT ![t] = "scheduled"]
            /\ budgetHit' = budgetHit
    ELSE /\ tpc' = tpc0
         /\ budgetHit' = TRUE

\* Semaphore#signal: value + 1 where value < limit. With no row, nothing.
Signal == IF sem = "taken" THEN "free" ELSE sem

\* A Monitor run's transaction writes the next row (monitor_changes).
Write ==
  /\ writer = "idle"
  /\ written < 2
  /\ written' = written + 1
  /\ writer' = "scheduling"
  /\ UNCHANGED <<status, attempts, sends, early, alone, tpc, queue, sem, budgetHit>>

\* After the commit: IntegrationNotification.schedule_ticks.
ScheduleAfterWrite ==
  /\ writer = "scheduling"
  /\ writer' = "idle"
  /\ Schedule(tpc)
  /\ UNCHANGED <<written, status, attempts, sends, early, alone, queue, sem>>

\* The run's process dies after its commit, before schedule_ticks.
WriterDies ==
  /\ WritersCanDie
  /\ writer = "scheduling"
  /\ writer' = "idle"
  /\ UNCHANGED <<written, status, attempts, sends, early, alone, tpc, queue, sem, budgetHit>>

\* The tick's job is dispatched: Semaphore#wait, or on_conflict: :discard.
Dispatch(t) ==
  /\ tpc[t] = "scheduled"
  /\ IF sem \in {"none", "free"}
       THEN /\ sem' = "taken"
            /\ tpc' = [tpc EXCEPT ![t] = "ready"]
       ELSE /\ tpc' = [tpc EXCEPT ![t] = "gone"]
            /\ UNCHANGED sem
  /\ UNCHANGED <<written, writer, status, attempts, sends, early, alone, queue, budgetHit>>

\* deliver_pending: row_ids = Notification.pending_for(...).pluck(:id).
\* deliver_monitor_rows then loads those rows by id, oldest first, and does
\* not read their status again. No rows: the job ends.
Pluck(t) ==
  /\ tpc[t] = "ready"
  /\ LET ids == {i \in Rows : Deliverable(i)}
         order == IF ids = {1, 2} THEN <<1, 2>> ELSE IF ids = {} THEN <<>> ELSE <<CHOOSE i \in ids : TRUE>>
     IN IF ids = {}
          THEN /\ tpc' = [tpc EXCEPT ![t] = "done"]
               /\ sem' = Signal
               /\ UNCHANGED queue
          ELSE /\ tpc' = [tpc EXCEPT ![t] = "deliver"]
               /\ queue' = [queue EXCEPT ![t] = order]
               /\ UNCHANGED sem
  /\ UNCHANGED <<written, writer, status, attempts, sends, early, alone, budgetHit>>

\* The chain loop: Notification.mark_rows([row.id], :processing) counts an
\* attempt. In the status chain, never_told? first: a running again whose
\* not running was given up is marked skipped, and the chain goes on.
Mark(t) ==
  /\ tpc[t] = "deliver"
  /\ queue[t] # <<>>
  /\ LET i == Head(queue[t])
     IN IF StatusChain /\ i = 2 /\ GivenUp(1)
          THEN /\ status' = [status EXCEPT ![i] = "skipped"]
               /\ queue' = [queue EXCEPT ![t] = Tail(queue[t])]
               /\ UNCHANGED <<attempts, tpc>>
          ELSE /\ status' = [status EXCEPT ![i] = "processing"]
               /\ attempts' = [attempts EXCEPT ![i] = attempts[i] + 1]
               /\ tpc' = [tpc EXCEPT ![t] = "send"]
               /\ UNCHANGED queue
  /\ UNCHANGED <<written, writer, sends, early, alone, sem, budgetHit>>

\* deliver_monitor_message: the provider gets the message and the row is sent,
\* or the provider fails, the row is failed, and the chain stops.
Send(t) ==
  /\ tpc[t] = "send"
  /\ LET i == Head(queue[t])
     IN \/ /\ sends' = [sends EXCEPT ![i] = sends[i] + 1]
           /\ status' = [status EXCEPT ![i] = "sent"]
           /\ early' = (early \/ (i = 2 /\ ~Settled(1) /\ sends[1] = 0))
           /\ alone' = (alone \/ (i = 2 /\ GivenUp(1)))
           /\ queue' = [queue EXCEPT ![t] = Tail(queue[t])]
           /\ tpc' = [tpc EXCEPT ![t] = "deliver"]
        \/ /\ SendCanFail
           /\ status' = [status EXCEPT ![i] = "failed"]
           /\ queue' = [queue EXCEPT ![t] = <<>>]
           /\ tpc' = [tpc EXCEPT ![t] = "deliver"]
           /\ UNCHANGED <<sends, early, alone>>
  /\ UNCHANGED <<written, writer, attempts, sem, budgetHit>>

\* After the chain: a follow-up tick when a row is still deliverable, then the
\* job ends and Solid Queue signals the semaphore.
Follow(t) ==
  /\ tpc[t] = "deliver"
  /\ queue[t] = <<>>
  /\ sem' = Signal
  /\ LET ended == [tpc EXCEPT ![t] = "done"]
     IN IF \E i \in Rows : Deliverable(i)
          THEN Schedule(ended)
          ELSE /\ tpc' = ended
               /\ UNCHANGED budgetHit
  /\ UNCHANGED <<written, writer, status, attempts, sends, early, alone, queue>>

\* The job's process dies. The supervisor fails the job
\* (ClaimedExecution.fail_all_with) and signals the semaphore.
TickDies(t) ==
  /\ TicksCanDie
  /\ tpc[t] \in {"deliver", "send"}
  /\ tpc' = [tpc EXCEPT ![t] = "done"]
  /\ queue' = [queue EXCEPT ![t] = <<>>]
  /\ sem' = Signal
  /\ UNCHANGED <<written, writer, status, attempts, sends, early, alone, budgetHit>>

\* Concurrency maintenance deletes the semaphore 2 minutes after it was taken.
Expire ==
  /\ LockCanExpire
  /\ sem = "taken"
  /\ sem' = "none"
  /\ UNCHANGED <<written, writer, status, attempts, sends, early, alone, tpc, queue, budgetHit>>

Quiet ==
  /\ writer = "idle"
  /\ \A t \in Ticks : tpc[t] \in {"unused", "gone", "done"}

Done ==
  /\ Quiet
  /\ written = 2
  /\ UNCHANGED vars

Next ==
  \/ Write \/ ScheduleAfterWrite \/ WriterDies \/ Expire
  \/ \E t \in Ticks : Dispatch(t) \/ Pluck(t) \/ Mark(t) \/ Send(t) \/ Follow(t) \/ TickDies(t)
  \/ Done

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ status \in [Rows -> {"pending", "processing", "sent", "failed", "skipped"}]
  /\ tpc \in [Ticks -> {"unused", "scheduled", "ready", "gone", "deliver", "send", "done"}]
  /\ sem \in {"none", "taken", "free"}

-----------------------------------------------------------------------------
(* The rules. *)

\* The provider gets each Notification once, not twice.
SentAtMostOnce == \A i \in Rows : sends[i] <= 1

\* A resolve never goes before the trigger it closes, while that trigger can
\* still go.
ResolveAfterTrigger == ~early

\* When no run and no tick has work left, each row written has its outcome:
\* sent, skipped, or failed for the last time. None waits for ever.
NoRowLeftBehind ==
  (Quiet /\ ~budgetHit) => \A i \in 1..written : Settled(i)

\* In the status chain: a running again never goes after the not running
\* before it was given up. The Integration was never told that the Monitors
\* stopped.
NoResolveAlone == ~alone
=============================================================================
