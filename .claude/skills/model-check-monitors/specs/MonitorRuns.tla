---------------------------- MODULE MonitorRuns ----------------------------
(***************************************************************************)
(* Runs of one Monitor: console/app/services/monitor_evaluation.rb.        *)
(*                                                                         *)
(* A run is a MonitorEvaluationJob that the scheduler enqueued, or Run now *)
(* (MonitorsController#run, McpTools::RunMonitor). Each step below is one  *)
(* statement or one transaction in the code, so TLC tries every order of   *)
(* them:                                                                   *)
(*                                                                         *)
(*   Load    LogMonitor.find_by / set_monitor, then now = Time.current     *)
(*   Take    MonitorEvaluation.take: one UPDATE of run_started_at          *)
(*   Read    EventStore.logs_ingested_after(after_batch: ingest_cursor),   *)
(*           or the Threshold Query over the window that ends at now       *)
(*   Commit  MonitorEvaluation#record: one IMMEDIATE transaction           *)
(*   Release the ensure in MonitorEvaluation.run: UPDATE ... WHERE         *)
(*           run_started_at = started                                      *)
(*                                                                         *)
(* The model keeps what both kinds share (the lease, the version check,   *)
(* last_run_at) and what a Match run adds (ingest_cursor and the Log       *)
(* batches that its monitor_matched row lists). A Threshold run's states   *)
(* come from the range that ends at its now, so NoStaleWrite stands for it.*)
(* Left out: Match timeouts and the skip after TIMEOUTS_BEFORE_SKIPPING,   *)
(* arming, EventStore errors (a failed run writes no cursor and no state), *)
(* and deletion of the Monitor.                                            *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
  Runs,           \* the runs that start, each once
  MaxBatches,     \* the Log batches that ingest stores
  MaxSaves,       \* the saves of the Monitor by a person
  LeaseCanExpire  \* TRUE: a run can take longer than RUN_LEASE (5 minutes)

None == "none"

VARIABLES
  head,      \* the newest Log batch in EventStore
  cursor,    \* log_monitors.ingest_cursor
  version,   \* log_monitors.updated_at: each save changes it, update_columns does not
  saves,
  lease,     \* the run whose started time is in log_monitors.run_started_at, or None
  expired,   \* TRUE when that time is older than RUN_LEASE
  clock,     \* Time.current
  lastNow,   \* the now of the last run that wrote what it found
  pc,        \* the step of each run
  snap,      \* what each run loaded: [cursor, version, now]
  readTo,    \* the ingest_cursor that EventStore answered to each run
  matched,   \* matched[b]: the monitor_matched rows that list Log batch b
  stale,     \* a run wrote what it found over an older range than a run before it
  overSave   \* a run wrote what it found over a save that it did not load

vars == <<head, cursor, version, saves, lease, expired, clock, lastNow,
          pc, snap, readTo, matched, stale, overSave>>

Batches == 1..MaxBatches

Init ==
  /\ head = 0
  /\ cursor = 0          \* armed at the newest batch
  /\ version = 0
  /\ saves = 0
  /\ lease = None
  /\ expired = FALSE
  /\ clock = 0
  /\ lastNow = 0
  /\ pc = [r \in Runs |-> "queued"]
  /\ snap = [r \in Runs |-> [cursor |-> 0, version |-> 0, now |-> 0]]
  /\ readTo = [r \in Runs |-> 0]
  /\ matched = [b \in Batches |-> 0]
  /\ stale = FALSE
  /\ overSave = FALSE

\* Log ingest and Log processing store the next batch.
Ingest ==
  /\ head < MaxBatches
  /\ head' = head + 1
  /\ UNCHANGED <<cursor, version, saves, lease, expired, clock, lastNow,
                 pc, snap, readTo, matched, stale, overSave>>

\* A person saves the Monitor (MonitorsController#update): updated_at changes,
\* and a change that stops the threshold check resolves its groups.
Save ==
  /\ saves < MaxSaves
  /\ saves' = saves + 1
  /\ version' = version + 1
  /\ UNCHANGED <<head, cursor, lease, expired, clock, lastNow,
                 pc, snap, readTo, matched, stale, overSave>>

\* MonitorEvaluationJob#perform: LogMonitor.find_by(id:), now = Time.current.
\* Run now: the set_monitor before_action, then Time.current.
Load(r) ==
  /\ pc[r] = "queued"
  /\ snap' = [snap EXCEPT ![r] = [cursor |-> cursor, version |-> version, now |-> clock]]
  /\ clock' = clock + 1
  /\ pc' = [pc EXCEPT ![r] = "loaded"]
  /\ UNCHANGED <<head, cursor, version, saves, lease, expired, lastNow,
                 readTo, matched, stale, overSave>>

\* MonitorEvaluation.take: UPDATE ... SET run_started_at = started
\* WHERE run_started_at IS NULL OR run_started_at < started - RUN_LEASE.
\* A run that finds the lease taken returns nil and does nothing.
Take(r) ==
  /\ pc[r] = "loaded"
  /\ IF lease = None \/ expired
       THEN /\ lease' = r
            /\ expired' = FALSE
            /\ pc' = [pc EXCEPT ![r] = "holding"]
       ELSE /\ pc' = [pc EXCEPT ![r] = "done"]
            /\ UNCHANGED <<lease, expired>>
  /\ UNCHANGED <<head, cursor, version, saves, clock, lastNow,
                 snap, readTo, matched, stale, overSave>>

\* run_match: EventStore.logs_ingested_after(after_batch: @monitor.ingest_cursor)
\* answers the Logs after the cursor that the run loaded, and the newest batch
\* as the new ingest_cursor.
Read(r) ==
  /\ pc[r] = "holding"
  /\ readTo' = [readTo EXCEPT ![r] = head]
  /\ pc' = [pc EXCEPT ![r] = "read"]
  /\ UNCHANGED <<head, cursor, version, saves, lease, expired, clock, lastNow,
                 snap, matched, stale, overSave>>

\* MonitorEvaluation#record: in one transaction, it reads updated_at again.
\* When it is the version that the run loaded, the run writes what it found:
\* notify_matched for the Logs after the loaded cursor, the new ingest_cursor
\* and last_run_at (or, for a Threshold run, the states from its range).
\* Otherwise it records CHANGED_DURING_RUN and writes nothing it found.
Commit(r) ==
  /\ pc[r] = "read"
  /\ LET writes == version = snap[r].version   \* record: elsif version != @version
     IN /\ IF writes
             THEN /\ matched' = [b \in Batches |->
                                  IF snap[r].cursor < b /\ b <= readTo[r]
                                    THEN matched[b] + 1 ELSE matched[b]]
                  /\ cursor' = readTo[r]
                  /\ lastNow' = snap[r].now
                  /\ stale' = (stale \/ snap[r].now < lastNow)
             ELSE UNCHANGED <<matched, cursor, lastNow, stale>>
        /\ overSave' = (overSave \/ (writes /\ version # snap[r].version))
  /\ pc' = [pc EXCEPT ![r] = "committed"]
  /\ UNCHANGED <<head, version, saves, lease, expired, clock, snap, readTo>>

\* The ensure in MonitorEvaluation.run:
\* UPDATE ... SET run_started_at = NULL WHERE run_started_at = started.
Release(r) ==
  /\ pc[r] = "committed"
  /\ IF lease = r
       THEN /\ lease' = None
            /\ expired' = FALSE
       ELSE UNCHANGED <<lease, expired>>
  /\ pc' = [pc EXCEPT ![r] = "done"]
  /\ UNCHANGED <<head, cursor, version, saves, clock, lastNow,
                 snap, readTo, matched, stale, overSave>>

\* RUN_LEASE passes while a run holds the Monitor.
Expire ==
  /\ LeaseCanExpire
  /\ lease # None
  /\ ~expired
  /\ expired' = TRUE
  /\ UNCHANGED <<head, cursor, version, saves, lease, clock, lastNow,
                 pc, snap, readTo, matched, stale, overSave>>

Done ==
  /\ \A r \in Runs : pc[r] = "done"
  /\ UNCHANGED vars

Next ==
  \/ Ingest
  \/ Save
  \/ Expire
  \/ \E r \in Runs : Load(r) \/ Take(r) \/ Read(r) \/ Commit(r) \/ Release(r)
  \/ Done

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ head \in 0..MaxBatches
  /\ cursor \in 0..MaxBatches
  /\ lease \in Runs \cup {None}
  /\ pc \in [Runs -> {"queued", "loaded", "holding", "read", "committed", "done"}]

-----------------------------------------------------------------------------
(* The rules. *)

\* Each Log that matches is in one monitor_matched Notification, not two.
NoLogMatchedTwice == \A b \in Batches : matched[b] <= 1

\* Each Log before the cursor was in a monitor_matched Notification.
NoLogMissed == \A b \in 1..cursor : matched[b] >= 1

\* No run writes what it found over an older range than the run before it.
\* For a Threshold monitor, that write moves its groups back in time: a group
\* that the newer run fired resolves again, and the next run fires it again.
NoStaleWrite == ~stale

\* A run that loaded the Monitor before a save writes nothing it found, so it
\* never undoes what the save did (LogMonitor#resolve_groups).
NoWriteOverASave == ~overSave

\* At most one run reads EventStore for the Monitor at a time.
OneRunAtATime ==
  \A r1, r2 \in Runs :
    (r1 # r2 /\ pc[r1] \in {"holding", "read"}) => pc[r2] \notin {"holding", "read"}
=============================================================================
