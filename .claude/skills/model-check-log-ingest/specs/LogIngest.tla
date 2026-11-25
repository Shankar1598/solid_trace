------------------------------ MODULE LogIngest ------------------------------
(***************************************************************************)
(* One Project's Logs, from ingest to ClickHouse, and the Match scan that   *)
(* reads them in arrival order.                                             *)
(*                                                                         *)
(*   Ingest    PebbleWriter.WriteLogBatch (NoSync), then the 200            *)
(*             (ingest/logs.go storeLogs)                                  *)
(*   Take      Processor.storeLogs: LogProcessingCursor, RecordedLogInsert, *)
(*             LogBatchesAfter, the again/upTo logic, then RecordLogInsert  *)
(*             (Sync) when the range is new                                *)
(*   Insert    ClickHouseLogs.InsertLogBatches with insert_deduplication_  *)
(*             token logs-<project>-<after>-<upTo>                          *)
(*   SetCursor SetLogProcessingCursor (NoSync)                             *)
(*   Delete    DeleteLogBatchesUpTo (NoSync)                               *)
(*   Restart   Processor.Prepare: DeleteLogBatchesUpTo(cursor)             *)
(*   Scan      ClickHouseLogs.ScanLogsIngestedAfter, as a Match monitor's   *)
(*             run calls it (handler/logs.go): it reads the cursor, then   *)
(*             the Logs of batches in (after, cursor], and answers the     *)
(*             cursor as ingest_cursor, which Console keeps                *)
(*                                                                         *)
(* Pebble is its write-ahead log: wal is every write in order, and durable *)
(* is how much of it is on disk. A Sync write puts all of wal on disk; the *)
(* OS can write back more at any time. A process crash keeps every write   *)
(* (the code's claim for NoSync); a power loss keeps only the durable      *)
(* prefix, since Pebble replays its WAL up to the first missing record.    *)
(* ClickHouse keeps each part it wrote; on a power loss it keeps the parts *)
(* that reached the disk, which are all of them when ClickHouseSyncsParts. *)
(* A part's dedup token goes with its part.                                *)
(*                                                                         *)
(* Each batch holds one Log, and its id is its number, in arrival order.   *)
(* Left out: the Log transform and content errors (storeApart), parts of   *)
(* more than one day, the key counts and Source counts tables (each has    *)
(* its own dedup log, with the same token), Retention, Project deletion,  *)
(* other Projects, and the id floor (KeepLogBatchIDsAbove).                *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
  N,                    \* the Log batches that ingest receives
  MaxPer,               \* the batches that one insert takes (maxLogsPerCommit)
  MaxScans,             \* the Match scans
  ProcessCanCrash,      \* TRUE: EventStore's process can die
  PowerCanFail,         \* TRUE: the machine can lose power or reset
  ClickHouseSyncsParts, \* TRUE: ClickHouse syncs each part to disk before it answers
  WindowCanPass         \* TRUE: a token can leave non_replicated_deduplication_window before its range goes again

Ids == 1..N

VARIABLES
  wal,        \* Pebble's writes, in order
  durable,    \* how many of them are on disk
  next,       \* the next batch that ingest receives
  accepted,   \* the batches answered 200
  up,         \* EventStore runs
  pc,         \* Log processing's step
  rng,        \* the range that it inserts: [after, upTo, ids]
  parts,      \* ClickHouse's parts of the logs table: [n, ids, tok, onDisk]
  made,       \* the parts made so far: each part's n, so that two equal parts stay two
  forgotten,  \* the parts whose dedup entry left the window
  onDisk,     \* the batches whose write reached Pebble's disk
  mc,         \* the Match monitor's ingest_cursor, in Console
  matched,    \* matched[b]: how often a Match scan read the Log of batch b
  fails,      \* the inserts that answered an error
  scans, crashes, losses

vars == <<wal, durable, next, accepted, up, pc, rng, parts, made, forgotten, onDisk, mc, matched, fails, scans, crashes, losses>>

Max(S) == CHOOSE x \in S : \A y \in S : y <= x
Min(S) == CHOOSE x \in S : \A y \in S : x <= y
Smallest(S, k) == {x \in S : Cardinality({y \in S : y < x}) < k}

\* What Pebble holds after the writes w: the batches, the Log processing
\* cursor (0 is uuid.Nil) and the recorded range (RecordedLogInsert).
StateOf(w) ==
  LET f[i \in 0..Len(w)] ==
        IF i = 0 THEN [batches |-> {}, cursor |-> 0, recorded |-> 0]
        ELSE LET s == f[i - 1]
                 op == w[i]
             IN CASE op.k = "batch"    -> [s EXCEPT !.batches = @ \cup {op.v}]
                  [] op.k = "cursor"   -> [s EXCEPT !.cursor = op.v]
                  [] op.k = "recorded" -> [s EXCEPT !.recorded = op.v]
                  [] op.k = "delete"   -> [s EXCEPT !.batches = {b \in @ : b > op.v}]
  IN f[Len(w)]

Pebble == StateOf(wal)

\* The batches written in the first d writes of w.
BatchesIn(w, d) == {w[i].v : i \in {j \in 1..d : w[j].k = "batch"}}
Remembered == {p.tok : p \in {q \in parts : q.n \notin forgotten}}
Copies(b) == Cardinality({p \in parts : b \in p.ids})

Init ==
  /\ wal = <<>>
  /\ durable = 0
  /\ next = 1
  /\ accepted = {}
  /\ up = TRUE
  /\ pc = "idle"
  /\ rng = [after |-> 0, upTo |-> 0, ids |-> {}]
  /\ parts = {}
  /\ made = 0
  /\ forgotten = {}
  /\ onDisk = {}
  /\ mc = 0
  /\ matched = [b \in Ids |-> 0]
  /\ fails = 0
  /\ scans = 0
  /\ crashes = 0
  /\ losses = 0

\* WriteLogBatch with pebble.NoSync, then the 200.
Ingest ==
  /\ up
  /\ next <= N
  /\ wal' = Append(wal, [k |-> "batch", v |-> next])
  /\ accepted' = accepted \cup {next}
  /\ next' = next + 1
  /\ UNCHANGED <<durable, up, pc, rng, parts, made, forgotten, onDisk, mc, matched, fails, scans, crashes, losses>>

\* storeLogs up to the insert. The range recorded past the cursor goes
\* again, the same batches under the same token; else the next batches
\* after the cursor, recorded with pebble.Sync first.
Take ==
  /\ up
  /\ pc = "idle"
  /\ LET s == Pebble
         after == {b \in s.batches : b > s.cursor}
         first == Smallest(after, MaxPer)
         kept == {b \in first : b <= s.recorded}
         again == s.recorded > s.cursor /\ kept # {}
         ids == IF again THEN kept ELSE first
     IN /\ ids # {}
        /\ rng' = [after |-> s.cursor, upTo |-> Max(ids), ids |-> ids]
        /\ IF again
             THEN UNCHANGED <<wal, durable, onDisk>>
             ELSE /\ wal' = Append(wal, [k |-> "recorded", v |-> Max(ids)])
                  /\ durable' = Len(wal) + 1
                  /\ onDisk' = onDisk \cup BatchesIn(wal', Len(wal) + 1)
  /\ pc' = "insert"
  /\ UNCHANGED <<next, accepted, up, parts, made, forgotten, mc, matched, fails, scans, crashes, losses>>

\* InsertLogBatches. ClickHouse drops a part whose token it remembers.
\* It answers, or the answer is lost after it wrote, or it writes nothing.
Insert ==
  /\ up
  /\ pc = "insert"
  /\ LET tok == <<rng.after, rng.upTo>>
         new == ~(tok \in Remembered)
         wrote == IF new THEN parts \cup {[n |-> made + 1, ids |-> rng.ids, tok |-> tok, onDisk |-> ClickHouseSyncsParts]}
                  ELSE parts
     IN \/ /\ parts' = wrote                 \* the insert returns
           /\ made' = IF new THEN made + 1 ELSE made
           /\ UNCHANGED fails
           /\ pc' = "stored"
        \/ /\ fails < 2                      \* two errors reach every path here
           /\ fails' = fails + 1
           /\ parts' = wrote                 \* it wrote, but the answer is an error
           /\ made' = IF new THEN made + 1 ELSE made
           /\ pc' = "idle"
        \/ /\ fails < 2
           /\ fails' = fails + 1
           /\ UNCHANGED <<parts, made>>      \* it failed and wrote nothing
           /\ pc' = "idle"
  /\ UNCHANGED <<wal, durable, next, accepted, up, rng, forgotten, onDisk, mc, matched, scans, crashes, losses>>

\* SetLogProcessingCursor with pebble.NoSync.
SetCursor ==
  /\ up
  /\ pc = "stored"
  /\ wal' = Append(wal, [k |-> "cursor", v |-> rng.upTo])
  /\ pc' = "cursorSet"
  /\ UNCHANGED <<durable, next, accepted, up, rng, parts, made, forgotten, onDisk, mc, matched, fails, scans, crashes, losses>>

\* DeleteLogBatchesUpTo with pebble.NoSync.
Delete ==
  /\ up
  /\ pc = "cursorSet"
  /\ wal' = Append(wal, [k |-> "delete", v |-> rng.upTo])
  /\ pc' = "idle"
  /\ UNCHANGED <<durable, next, accepted, up, rng, parts, made, forgotten, onDisk, mc, matched, fails, scans, crashes, losses>>

\* A Match monitor's run: ScanLogsIngestedAfter(after: ingest_cursor). With
\* after at or past the cursor it reads nothing; either way it answers the
\* cursor, and Console keeps it.
Scan ==
  /\ up
  /\ scans < MaxScans
  /\ LET c == Pebble.cursor
     IN /\ matched' = [b \in Ids |-> IF mc < b /\ b <= c THEN matched[b] + Copies(b) ELSE matched[b]]
        /\ mc' = IF mc >= c THEN mc ELSE c   \* a cursor behind after answers after
  /\ scans' = scans + 1
  /\ UNCHANGED <<wal, durable, next, accepted, up, pc, rng, parts, made, forgotten, onDisk, fails, crashes, losses>>

\* The OS writes back Pebble's WAL, or a ClickHouse part. (Only a power
\* loss can tell, so these steps run only when one can happen.)
WritebackWal ==
  /\ PowerCanFail
  /\ durable < Len(wal)
  /\ durable' = Len(wal)
  /\ onDisk' = onDisk \cup BatchesIn(wal, Len(wal))
  /\ UNCHANGED <<wal, next, accepted, up, pc, rng, parts, made, forgotten, mc, matched, fails, scans, crashes, losses>>

WritebackPart ==
  /\ PowerCanFail
  /\ \E p \in parts :
       /\ ~p.onDisk
       /\ parts' = (parts \ {p}) \cup {[p EXCEPT !.onDisk = TRUE]}
  /\ UNCHANGED <<made, wal, durable, next, accepted, up, pc, rng, forgotten, onDisk, mc, matched, fails, scans, crashes, losses>>

\* Other inserts push a token out of the dedup window (logsDedupWindow blocks).
Forget ==
  /\ WindowCanPass
  /\ forgotten = {}                     \* one is enough to show it
  /\ \E p \in parts : forgotten' = {p.n}
  /\ UNCHANGED <<wal, durable, next, accepted, up, pc, rng, parts, made, onDisk, mc, matched, fails, scans, crashes, losses>>

Crash ==
  /\ ProcessCanCrash
  /\ up
  /\ crashes < 1
  /\ up' = FALSE
  /\ pc' = "down"
  /\ crashes' = crashes + 1
  /\ UNCHANGED <<wal, durable, next, accepted, rng, parts, made, forgotten, onDisk, mc, matched, fails, scans, losses>>

PowerLoss ==
  /\ PowerCanFail
  /\ losses < 1
  /\ wal' = SubSeq(wal, 1, durable)
  /\ parts' = {p \in parts : p.onDisk}
  /\ up' = FALSE
  /\ pc' = "down"
  /\ losses' = losses + 1
  /\ UNCHANGED <<made, durable, next, accepted, rng, forgotten, onDisk, mc, matched, fails, scans, crashes>>

\* Processor.Prepare at the start: it deletes the batches at or below the cursor.
Restart ==
  /\ ~up
  /\ up' = TRUE
  /\ pc' = "idle"
  /\ wal' = IF Pebble.cursor > 0 THEN Append(wal, [k |-> "delete", v |-> Pebble.cursor]) ELSE wal
  /\ UNCHANGED <<durable, next, accepted, rng, parts, made, forgotten, onDisk, mc, matched, fails, scans, crashes, losses>>

Done ==
  /\ up
  /\ next > N
  /\ pc = "idle"
  /\ {b \in Pebble.batches : b > Pebble.cursor} = {}
  /\ UNCHANGED vars

Next ==
  \/ Ingest \/ Take \/ Insert \/ SetCursor \/ Delete \/ Scan
  \/ WritebackWal \/ WritebackPart \/ Forget
  \/ Crash \/ PowerLoss \/ Restart
  \/ Done

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ pc \in {"idle", "insert", "stored", "cursorSet", "down"}
  /\ durable <= Len(wal)
  /\ mc \in 0..N

-----------------------------------------------------------------------------
(* The rules. *)

\* Each Log is in ClickHouse at most once.
NoLogStoredTwice == \A b \in Ids : Copies(b) <= 1

\* Each Log whose batch reached Pebble's disk is in ClickHouse, or still in
\* Pebble for Log processing. This is what Pebble in front of the 200 gives.
NoLoggedLogLost ==
  \A b \in onDisk : Copies(b) >= 1 \/ (b \in Pebble.batches /\ b > Pebble.cursor)

\* Each Log answered 200 is in ClickHouse, or still in Pebble for Log
\* processing. Ingest writes with pebble.NoSync, so a power loss can lose
\* a Log that was answered 200 and not yet on disk: the code accepts that.
NoAcceptedLogLost ==
  \A b \in accepted : Copies(b) >= 1 \/ (b \in Pebble.batches /\ b > Pebble.cursor)

\* A Match monitor reads each Log once, not twice.
MatchReadsEachLogOnce == \A b \in Ids : matched[b] <= 1

\* A Match monitor has read each Log before its ingest_cursor whose batch
\* reached Pebble's disk.
MatchMissesNoLog == \A b \in onDisk : b <= mc => matched[b] >= 1
=============================================================================
