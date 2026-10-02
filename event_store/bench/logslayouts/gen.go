// Package logslayouts is a throwaway benchmark of the hot-table layouts for
// Logs shortlisted in .scratch/logs/issues/03 (ticket 09): Attributes as JSON
// text, as VARIANT, or as typed MAPs, then hourly Parquet written from each.
package logslayouts

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

const (
	// TargetBytesPerSec is the target ingest volume (3–5 TB/month).
	TargetBytesPerSec = 1_900_000
	// LogsPerSecond sets the logical clock: Log i is stamped i/LogsPerSecond
	// seconds after Base, so the data looks like the target rate (about 600 B
	// per Log) whatever rate it was written at.
	LogsPerSecond = 3200
	// TailKeys is the number of distinct long-tail Attribute keys.
	TailKeys = 2000
)

// Base is the logical time of Log 0.
var Base = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// Query targets. The generator test checks they are as selective as intended.
var (
	CommonKey   = "http.method"
	CommonValue = "POST"
	RareKey     = tailKeyName(124) // a string-valued tail key, in about 0.1% of Logs
	RareValue   = "t3"
	MixedKey    = "mixed.size"
	MixedLo     = 100.0
	MixedHi     = 200.0
	GroupKey    = "service.version"
	Needle      = "connection reset by peer"
)

type Kind uint8

const (
	KindString Kind = iota
	KindInt
	KindFloat
	KindBool
	// KindArray is a list of strings; the MAP layout keeps it as JSON text.
	KindArray
)

type Value struct {
	Kind Kind
	S    string
	I    int64
	F    float64
	B    bool
	A    []string
}

// JSON returns the value as encoding/json decodes it: string, float64, bool
// or []any.
func (v Value) JSON() any {
	switch v.Kind {
	case KindInt:
		return float64(v.I)
	case KindFloat:
		return v.F
	case KindBool:
		return v.B
	case KindArray:
		a := make([]any, len(v.A))
		for i, s := range v.A {
			a[i] = s
		}
		return a
	}
	return v.S
}

type Attr struct {
	Key   string
	Value Value
}

type Log struct {
	ID          int64
	ProjectID   int32
	Time        time.Time
	Level       string
	Message     string
	Service     string
	Environment string
	TraceID     string
	SpanID      string
	Attrs       []Attr
	AttrsJSON   []byte
	// Size approximates the Log's uncompressed size as one NDJSON line.
	Size int
}

func (l Log) AttrsMap() map[string]any {
	m := make(map[string]any, len(l.Attrs))
	for _, a := range l.Attrs {
		m[a.Key] = a.Value.JSON()
	}
	return m
}

type Generator struct {
	seed uint64
}

func NewGenerator(seed uint64) *Generator {
	return &Generator{seed: seed}
}

// Log returns Log id. It depends only on the seed and id, so any Log can be
// regenerated to check a query result.
func (g *Generator) Log(id int64) Log {
	r := rand.New(rand.NewPCG(g.seed, uint64(id)))
	l := Log{
		ID:          id,
		ProjectID:   pick(r, projects),
		Time:        Base.Add((time.Duration(id) * time.Second / LogsPerSecond).Truncate(time.Microsecond)),
		Level:       pick(r, levels),
		Service:     services[r.IntN(len(services))],
		Environment: pick(r, environments),
	}
	l.Message = message(r)
	if r.Float64() < 0.02 {
		l.TraceID = fmt.Sprintf("%016x%016x", r.Uint64(), r.Uint64())
		l.SpanID = fmt.Sprintf("%016x", r.Uint64())
	}
	l.Attrs = attrs(r)
	l.AttrsJSON = encodeAttrs(l.Attrs)
	l.Size = 90 + len(l.Level) + len(l.Message) + len(l.Service) + len(l.Environment) +
		len(l.TraceID) + len(l.SpanID) + len(l.AttrsJSON)
	return l
}

type weighted[T any] struct {
	v T
	w float64
}

func pick[T any](r *rand.Rand, opts []weighted[T]) T {
	x := r.Float64()
	for _, o := range opts {
		if x < o.w {
			return o.v
		}
		x -= o.w
	}
	return opts[len(opts)-1].v
}

var (
	projects     = []weighted[int32]{{1, 0.7}, {2, 0.2}, {3, 0.1}}
	levels       = []weighted[string]{{"trace", 0.05}, {"debug", 0.15}, {"info", 0.60}, {"warn", 0.12}, {"error", 0.07}, {"fatal", 0.01}}
	environments = []weighted[string]{{"production", 0.8}, {"staging", 0.15}, {"development", 0.05}}
	services     = []string{"api", "web", "worker", "billing", "search", "auth"}
)

var messageHeads = []string{
	"request completed", "request failed", "user signed in", "user signed out",
	"cache miss for key", "cache hit for key", "retrying job", "job finished",
	"payment processed", "payment declined", "query executed", "slow query detected",
	"upstream returned error", Needle, "rate limit exceeded", "token refreshed",
	"webhook delivered", "webhook failed", "file uploaded", "email queued",
	"order created", "order shipped", "search performed", "index rebuilt",
	"session expired", "permission denied", "config reloaded", "health check ok",
	"message consumed", "message published", "lock acquired", "lock released",
	"invoice generated", "report exported", "feature flag evaluated", "worker started",
	"worker stopped", "migration applied", "backup completed", "dns lookup took long",
}

var fillerWords = strings.Fields(`the a for with from to in on at by after before during while
user account order request response service database table index cache queue worker job
payment invoice customer session token key value field record batch page item event
status code error warning retry attempt duration latency bytes rows result total count
id ref path route method region zone host pod node cluster shard replica primary
started finished took returned received sent created updated deleted skipped ignored`)

// message returns 50–500 bytes: a head phrase padded with filler words, mean
// length about 185.
func message(r *rand.Rand) string {
	n := min(500, 50+int(r.ExpFloat64()*140))
	var b strings.Builder
	b.Grow(n + 16)
	b.WriteString(messageHeads[r.IntN(len(messageHeads))])
	for b.Len() < n {
		b.WriteByte(' ')
		b.WriteString(fillerWords[r.IntN(len(fillerWords))])
	}
	return b.String()[:n]
}

type commonKey struct {
	name string
	p    float64
	gen  func(r *rand.Rand) Value
}

func str(s string) Value  { return Value{Kind: KindString, S: s} }
func num(i int64) Value   { return Value{Kind: KindInt, I: i} }
func flt(f float64) Value { return Value{Kind: KindFloat, F: math.Round(f*100) / 100} }
func oneOf(vs ...string) func(*rand.Rand) Value {
	return func(r *rand.Rand) Value { return str(vs[r.IntN(len(vs))]) }
}
func prefixed(prefix string, n int) func(*rand.Rand) Value {
	return func(r *rand.Rand) Value { return str(prefix + strconv.Itoa(r.IntN(n))) }
}
func intUpTo(n int) func(*rand.Rand) Value {
	return func(r *rand.Rand) Value { return num(int64(r.IntN(n))) }
}
func expFloat(mean float64) func(*rand.Rand) Value {
	return func(r *rand.Rand) Value { return flt(r.ExpFloat64() * mean) }
}
func boolean(p float64) func(*rand.Rand) Value {
	return func(r *rand.Rand) Value { return Value{Kind: KindBool, B: r.Float64() < p} }
}

// mixedValue is a number in half the Logs and a non-numeric string in the rest.
func mixedValue(n int) func(*rand.Rand) Value {
	return func(r *rand.Rand) Value {
		if r.IntN(2) == 0 {
			return num(int64(r.IntN(n)))
		}
		return oneOf("n/a", "unknown", "large", "small")(r)
	}
}

// commonKeys are the 41 common Attribute keys, two of them (5%) mixed-type and
// one an array. Their probabilities give about 10 common Attributes per Log.
var commonKeys = []commonKey{
	{"http.method", 0.6, func(r *rand.Rand) Value {
		return str(pick(r, []weighted[string]{{"GET", 0.6}, {"POST", 0.25}, {"PUT", 0.07}, {"DELETE", 0.05}, {"PATCH", 0.03}}))
	}},
	{"http.status_code", 0.6, func(r *rand.Rand) Value {
		return num(pick(r, []weighted[int64]{{200, 0.8}, {201, 0.04}, {204, 0.03}, {301, 0.02}, {400, 0.03}, {401, 0.02}, {403, 0.01}, {404, 0.03}, {500, 0.01}, {502, 0.005}, {503, 0.005}}))
	}},
	{"http.route", 0.6, prefixed("/api/v1/resource", 50)},
	{"http.duration_ms", 0.6, expFloat(120)},
	{"user.id", 0.6, prefixed("u", 100_000)},
	{"request.id", 0.6, func(r *rand.Rand) Value { return str(fmt.Sprintf("%016x", r.Uint64())) }},
	{"host.name", 0.6, prefixed("host-", 40)},
	{"region", 0.6, oneOf("us-east-1", "us-west-2", "eu-west-1", "eu-central-1", "ap-south-1", "ap-southeast-2")},
	{"service.version", 0.6, prefixed("v1.4.", 30)},
	{"k8s.pod", 0.6, prefixed("pod-", 200)},
	{"k8s.namespace", 0.2, oneOf("default", "prod", "jobs", "ingress", "monitoring")},
	{"db.system", 0.2, oneOf("postgres", "redis", "mysql")},
	{"db.duration_ms", 0.2, expFloat(15)},
	{"db.rows", 0.2, intUpTo(5000)},
	{"cache.hit", 0.2, boolean(0.7)},
	{"feature.flag", 0.2, prefixed("flag_", 20)},
	{"tenant.id", 0.2, prefixed("tenant-", 1000)},
	{"customer.plan", 0.2, oneOf("free", "pro", "enterprise")},
	{"user_agent", 0.2, oneOf(
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0",
		"curl/8.9.1", "python-requests/2.32.3", "okhttp/4.12.0")},
	{"client.ip", 0.2, func(r *rand.Rand) Value {
		return str(fmt.Sprintf("10.%d.%d.%d", r.IntN(256), r.IntN(256), r.IntN(256)))
	}},
	{"error.type", 0.1, oneOf("TimeoutError", "ConnectionError", "ValidationError", "NotFound", "PermissionDenied")},
	{"error.code", 0.1, intUpTo(100)},
	{"retry.count", 0.1, intUpTo(5)},
	{"queue.name", 0.1, oneOf("default", "mailers", "critical", "low", "webhooks")},
	{"job.id", 0.1, func(r *rand.Rand) Value { return str(fmt.Sprintf("job_%012x", r.Uint64()>>16)) }},
	{"bytes.sent", 0.1, intUpTo(1_000_000)},
	{"bytes.received", 0.1, intUpTo(1_000_000)},
	{"thread.name", 0.1, prefixed("thread-", 16)},
	{"logger.name", 0.1, prefixed("app.module", 30)},
	{"code.function", 0.1, prefixed("handle_", 100)},
	{"code.lineno", 0.1, intUpTo(2000)},
	{"session.id", 0.1, func(r *rand.Rand) Value { return str(fmt.Sprintf("s_%012x", r.Uint64()>>16)) }},
	{"order.id", 0.1, prefixed("ord_", 1_000_000)},
	{"payment.amount", 0.1, expFloat(80)},
	{"items.count", 0.1, intUpTo(50)},
	{"latency.p99", 0.1, expFloat(300)},
	{"gc.pause_ms", 0.1, expFloat(5)},
	{"sampled", 0.1, boolean(0.1)},
	{MixedKey, 0.3, mixedValue(1000)},
	{"mixed.status", 0.1, mixedValue(10)},
	{"tags", 0.05, func(r *rand.Rand) Value {
		a := make([]string, 1+r.IntN(3))
		for i := range a {
			a[i] = "tag-" + strconv.Itoa(r.IntN(20))
		}
		return Value{Kind: KindArray, A: a}
	}},
}

// tailCounts is how many long-tail Attributes a Log carries (mean 1.4).
var tailCounts = []weighted[int]{{0, 0.3}, {1, 0.3}, {2, 0.2}, {3, 0.12}, {4, 0.08}}

func tailKeyName(rank int) string { return fmt.Sprintf("ext.k%04d", rank) }

func isTailKey(k string) bool { return strings.HasPrefix(k, "ext.k") }

var tailNames = func() []string {
	n := make([]string, TailKeys)
	for i := range n {
		n[i] = tailKeyName(i)
	}
	return n
}()

// tailValue gives each tail key a fixed type by rank; every 20th is mixed.
func tailValue(r *rand.Rand, rank int) Value {
	if rank%20 == 0 {
		return mixedValue(1000)(r)
	}
	switch rank % 4 {
	case 1:
		return num(int64(r.IntN(1000)))
	case 2:
		return flt(r.Float64() * 100)
	case 3:
		return Value{Kind: KindBool, B: r.IntN(2) == 0}
	}
	return str("t" + strconv.Itoa(r.IntN(10)))
}

func attrs(r *rand.Rand) []Attr {
	out := make([]Attr, 0, 16)
	for _, k := range commonKeys {
		if r.Float64() < k.p {
			out = append(out, Attr{k.name, k.gen(r)})
		}
	}
	n := pick(r, tailCounts)
	if n == 0 {
		return out
	}
	zipf := rand.NewZipf(r, 1.1, 1, TailKeys-1)
	seen := make([]int, 0, n)
next:
	for range n {
		rank := int(zipf.Uint64())
		for _, s := range seen {
			if s == rank {
				continue next
			}
		}
		seen = append(seen, rank)
		out = append(out, Attr{tailNames[rank], tailValue(r, rank)})
	}
	return out
}

func encodeAttrs(as []Attr) []byte {
	b := make([]byte, 0, 32*len(as)+2)
	b = append(b, '{')
	for i, a := range as {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendJSONString(b, a.Key)
		b = append(b, ':')
		switch a.Value.Kind {
		case KindString:
			b = appendJSONString(b, a.Value.S)
		case KindInt:
			b = strconv.AppendInt(b, a.Value.I, 10)
		case KindFloat:
			b = strconv.AppendFloat(b, a.Value.F, 'f', -1, 64)
		case KindBool:
			b = strconv.AppendBool(b, a.Value.B)
		case KindArray:
			b = appendJSONArray(b, a.Value.A)
		}
	}
	return append(b, '}')
}

func appendJSONArray(b []byte, a []string) []byte {
	b = append(b, '[')
	for i, s := range a {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendJSONString(b, s)
	}
	return append(b, ']')
}

// appendJSONString quotes s; the generator only produces printable ASCII.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '"' || c == '\\' {
			b = append(b, '\\', c)
		} else {
			b = append(b, c)
		}
	}
	return append(b, '"')
}
