# 08: Which ingest sources ship in v1, with what auth and limits?

Type: grilling
Status: open
Blocked by: 02, 07
Map: [SolidTrace Logs](../map.md)

## Question

Which ingest endpoints ship in v1, how shippers authenticate (existing project keys, bearer tokens, a new ingest token), what request and field limits apply, and how overload is signalled to shippers?

Input (2026-09-26): the event store now has two listeners. Ingest routes register in `RegisterIngestRoutes` on the public listener; query routes live on a loopback-only listener. Key lookup returns 401 only for an unknown key and 500 when the lookup fails, so SDKs retry.
