package storage

import (
	"cmp"
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/solidtrace/event_store/models"
)

// MaxPromotedKeys is how many (type, key) pairs a Project's promoted set
// holds for one day.
const MaxPromotedKeys = 100

// keyCountsKept is how many keys' popularity counts are kept per Project per
// day: a top-K, wide enough that the daily set's top 100 come from it.
const keyCountsKept = 1000

// AttributeType is the type of an Attribute's value. It decides which of a
// hot table's maps holds the value, and the type of its column if promoted.
type AttributeType string

const (
	StringAttribute AttributeType = "string"
	NumberAttribute AttributeType = "number"
	BoolAttribute   AttributeType = "bool"
	// JSONAttribute values (arrays, deep nesting) are JSON text, and never
	// promoted.
	JSONAttribute AttributeType = "json"
)

// Map is the name of the map that holds Attributes of this type when they
// aren't promoted, such as attrs_string.
func (t AttributeType) Map() string {
	return "attrs_" + string(t)
}

// columnType is the DuckDB type of a promoted column of this type.
func (t AttributeType) columnType() string {
	switch t {
	case NumberAttribute:
		return "DOUBLE"
	case BoolAttribute:
		return "BOOLEAN"
	default:
		return "VARCHAR"
	}
}

// promotedTypes are the Attribute types that can be promoted.
var promotedTypes = []AttributeType{BoolAttribute, NumberAttribute, StringAttribute}

// PromotedKey is one (type, key) pair of a Project's promoted set for a day.
type PromotedKey struct {
	Type AttributeType
	Key  string
	// AddedAt is when Log processing put the key in the day's set, by its
	// clock. A key the set started with was added when the set was made.
	AddedAt time.Time
	// Partway is true when the key joined the day's set after some of the
	// day's Logs were stored. Those Logs may hold it in its map, so on that
	// day read it as coalesce(column, map[key]). Otherwise every Log of the
	// day that has the key holds it in the column, and never in the map.
	Partway bool
}

// Column is the name of the key's column in the Project's hot table, such as
// attrs_string.http.status. Quote it when using it as an identifier.
func (k PromotedKey) Column() string {
	return typedKey{k.Type, k.Key}.column()
}

// PromotedSet is a Project's promoted set for one UTC day: the Attributes
// whose values that day's Logs hold in columns of their own rather than in
// the maps. Logs of other days follow their own day's set.
type PromotedSet struct {
	Day  time.Time     // midnight UTC
	Keys []PromotedKey // by type, then key
}

// Lookup returns the promoted key of type t named key, if it is in the set.
func (s PromotedSet) Lookup(t AttributeType, key string) (PromotedKey, bool) {
	i, found := slices.BinarySearchFunc(s.Keys, typedKey{t, key}, func(k PromotedKey, target typedKey) int {
		return typedKey{k.Type, k.Key}.compare(target)
	})
	if !found {
		return PromotedKey{}, false
	}
	return s.Keys[i], true
}

// LogDay is the UTC day of a Log's time t, as midnight UTC.
func LogDay(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

// PromotedSet returns a Project's promoted set for the UTC day of t, from
// memory. A day without one has no keys: its Logs hold every Attribute in the
// maps. The keys are shared; don't modify them.
func (s *LogStore) PromotedSet(projectID uint32, t time.Time) PromotedSet {
	day := LogDay(t)
	s.promoted.mu.RLock()
	defer s.promoted.mu.RUnlock()
	if set, ok := s.promoted.sets[projectID][day]; ok {
		return set
	}
	return PromotedSet{Day: day}
}

// PromotedSets returns a Project's promoted sets for the UTC days from that of
// from to that of to, inclusive, oldest first, from memory. Days without a set
// are left out. The keys are shared; don't modify them.
//
// The promoted sets are how a reader learns which columns a day's Logs have,
// with no need to read a table's or a file's schema.
func (s *LogStore) PromotedSets(projectID uint32, from, to time.Time) []PromotedSet {
	first, last := LogDay(from), LogDay(to)
	s.promoted.mu.RLock()
	defer s.promoted.mu.RUnlock()
	var sets []PromotedSet
	for day, set := range s.promoted.sets[projectID] {
		if !day.Before(first) && !day.After(last) {
			sets = append(sets, set)
		}
	}
	slices.SortFunc(sets, func(a, b PromotedSet) int { return a.Day.Compare(b.Day) })
	return sets
}

// promotedSetCache is the promoted-set table in memory: every committed set,
// by Project and day.
type promotedSetCache struct {
	mu   sync.RWMutex
	sets map[uint32]map[time.Time]PromotedSet
}

// put replaces the sets of their days.
func (c *promotedSetCache) put(projectID uint32, sets []PromotedSet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, set := range sets {
		if c.sets[projectID] == nil {
			c.sets[projectID] = make(map[time.Time]PromotedSet)
		}
		c.sets[projectID][set.Day] = set
	}
}

// loadPromotedSets reads the whole promoted-set table.
func loadPromotedSets(db *sql.DB) (*promotedSetCache, error) {
	rows, err := db.Query("SELECT project_id, day, type, key, added_at, partway FROM logs.log_promoted_sets")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make(map[uint32]map[time.Time][]PromotedKey)
	for rows.Next() {
		var (
			projectID uint32
			day       time.Time
			key       PromotedKey
		)
		if err := rows.Scan(&projectID, &day, &key.Type, &key.Key, &key.AddedAt, &key.Partway); err != nil {
			return nil, err
		}
		key.AddedAt = key.AddedAt.UTC()
		if keys[projectID] == nil {
			keys[projectID] = make(map[time.Time][]PromotedKey)
		}
		day = LogDay(day)
		keys[projectID][day] = append(keys[projectID][day], key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	cache := &promotedSetCache{sets: make(map[uint32]map[time.Time]PromotedSet)}
	for projectID, days := range keys {
		for day, dayKeys := range days {
			cache.put(projectID, []PromotedSet{newPromotedSet(day, dayKeys)})
		}
	}
	return cache, nil
}

func newPromotedSet(day time.Time, keys []PromotedKey) PromotedSet {
	slices.SortFunc(keys, func(a, b PromotedKey) int {
		return typedKey{a.Type, a.Key}.compare(typedKey{b.Type, b.Key})
	})
	return PromotedSet{Day: day, Keys: keys}
}

// typedKey is an Attribute's (type, key) pair, the unit of promotion.
type typedKey struct {
	Type AttributeType
	Key  string
}

func (k typedKey) column() string {
	return k.Type.Map() + "." + k.Key
}

func (k typedKey) compare(other typedKey) int {
	return cmp.Or(cmp.Compare(k.Type, other.Type), cmp.Compare(k.Key, other.Key))
}

// promotable reports whether a key can be a column name. DuckDB can't parse
// a NUL in an identifier, and an invalid UTF-8 name breaks its catalog.
func promotable(key string) bool {
	return !strings.ContainsRune(key, 0) && utf8.ValidString(key)
}

// logKeys returns a Log's promotable (type, key) pairs, by type then key.
func logKeys(log models.Log) []typedKey {
	var keys []typedKey
	for _, t := range promotedTypes {
		for _, key := range slices.Sorted(attributeNames(log.Attributes, t)) {
			if promotable(key) {
				keys = append(keys, typedKey{t, key})
			}
		}
	}
	return keys
}

// attributeNames iterates the names of a Log's Attributes of type t.
func attributeNames(a models.Attributes, t AttributeType) iter.Seq[string] {
	switch t {
	case StringAttribute:
		return maps.Keys(a.String)
	case NumberAttribute:
		return maps.Keys(a.Number)
	case BoolAttribute:
		return maps.Keys(a.Bool)
	default:
		return maps.Keys(a.JSON)
	}
}

// attributeValue returns the value of a Log's Attribute of type t named key.
func attributeValue(a models.Attributes, k typedKey) (any, bool) {
	switch k.Type {
	case StringAttribute:
		v, ok := a.String[k.Key]
		return v, ok
	case NumberAttribute:
		v, ok := a.Number[k.Key]
		return v, ok
	case BoolAttribute:
		v, ok := a.Bool[k.Key]
		return v, ok
	default:
		v, ok := a.JSON[k.Key]
		return v, ok
	}
}

// dayPromotion is one day's promoted set as a Log processing transaction
// builds it.
type dayPromotion struct {
	day  time.Time
	keys map[typedKey]PromotedKey
	// isNew is true when the day had no promoted keys before this
	// transaction. Any of its Logs stored earlier had no key that could be
	// promoted, so none holds a key this transaction adds in its map.
	isNew bool
	added []PromotedKey // keys this transaction adds, to record
}

func (d *dayPromotion) set() PromotedSet {
	return newPromotedSet(d.day, slices.Collect(maps.Values(d.keys)))
}

// promotion is the promoted sets of the days of a Project's Logs in one Log
// processing transaction, and the hot-table columns they need.
type promotion struct {
	projectID uint32
	now       time.Time
	days      map[time.Time]*dayPromotion
	// columns are the hot table's columns and those this transaction adds,
	// by lower-case name: DuckDB compares column names ignoring ASCII case.
	columns map[string]string
	// existing are the hot table's columns before this transaction.
	existing map[string]bool
}

// promote does steps 1 and 2 of Log processing for a Project's Logs, inside
// conn's open transaction: it makes the promoted set of each day that has
// none yet, promotes the Logs' new keys while their day's set has room, adds
// the hot-table columns the sets need and records the sets' new keys.
func promote(ctx context.Context, conn *sql.Conn, projectID uint32, logs []models.Log, now time.Time) (*promotion, error) {
	p := &promotion{
		projectID: projectID,
		now:       now,
		days:      make(map[time.Time]*dayPromotion),
		columns:   make(map[string]string),
		existing:  make(map[string]bool),
	}
	columns, err := tableColumns(ctx, conn, hotTable(projectID))
	if err != nil {
		return nil, err
	}
	for _, column := range columns {
		p.columns[strings.ToLower(column)] = column
		p.existing[column] = true
	}

	if len(logs) == 0 {
		return p, nil
	}
	byDay := make(map[time.Time][]models.Log)
	for _, log := range logs {
		day := LogDay(log.Time)
		byDay[day] = append(byDay[day], log)
	}
	days := slices.SortedFunc(maps.Keys(byDay), time.Time.Compare)
	stored, err := p.storedSets(ctx, conn, days[0], days[len(days)-1])
	if err != nil {
		return nil, err
	}

	// Oldest day first, each day's keys promoted before the next day's set
	// is made, as a later day's set can start from an earlier one's.
	for _, day := range days {
		d, err := p.daySet(ctx, conn, day, stored[day])
		if err != nil {
			return nil, err
		}
		p.days[day] = d
		for _, log := range byDay[day] {
			for _, key := range logKeys(log) {
				p.add(d, key, !d.isNew)
			}
		}
	}

	if err := p.addColumns(ctx, conn); err != nil {
		return nil, err
	}
	return p, p.record(conn)
}

// daySet returns a day's promoted set from its stored keys, or makes it if it
// has none.
func (p *promotion) daySet(ctx context.Context, conn *sql.Conn, day time.Time, stored []PromotedKey) (*dayPromotion, error) {
	if len(stored) == 0 {
		return p.newDay(ctx, conn, day)
	}
	d := &dayPromotion{day: day, keys: make(map[typedKey]PromotedKey)}
	for _, key := range stored {
		d.keys[typedKey{key.Type, key.Key}] = key
		p.claimColumn(typedKey{key.Type, key.Key})
	}
	return d, nil
}

// newDay makes the promoted set of a day that has none: the set of the most
// recent day before it unchanged if that set wasn't full, else the top keys
// from the most recent day before it with counts. With no earlier set, it
// starts empty.
func (p *promotion) newDay(ctx context.Context, conn *sql.Conn, day time.Time) (*dayPromotion, error) {
	d := &dayPromotion{day: day, keys: make(map[typedKey]PromotedKey), isNew: true}
	previous, err := p.previousSet(ctx, conn, day)
	if err != nil {
		return nil, err
	}
	if len(previous) < MaxPromotedKeys {
		for _, key := range previous {
			p.add(d, key, false)
		}
		return d, nil
	}

	ranked, err := p.rankedKeys(ctx, conn, day)
	if err != nil {
		return nil, err
	}
	for _, key := range ranked {
		p.add(d, key, false)
	}
	return d, nil
}

// add puts key in a day's set if it isn't in it, the set has room and the
// key can have a column of its own.
func (p *promotion) add(d *dayPromotion, key typedKey, partway bool) {
	if _, ok := d.keys[key]; ok {
		return
	}
	if len(d.keys) >= MaxPromotedKeys || key.Type == JSONAttribute || !promotable(key.Key) || !p.claimColumn(key) {
		return
	}
	promoted := PromotedKey{Type: key.Type, Key: key.Key, AddedAt: p.now, Partway: partway}
	d.keys[key] = promoted
	d.added = append(d.added, promoted)
}

// claimColumn reserves key's column name, and reports false if another
// column already has it in a different case.
func (p *promotion) claimColumn(key typedKey) bool {
	column := key.column()
	lower := strings.ToLower(column)
	if other, ok := p.columns[lower]; ok {
		return other == column
	}
	p.columns[lower] = column
	return true
}

// promotedColumns are the columns of every key in the sets of the
// transaction's days, by name.
func (p *promotion) promotedColumns() []typedKey {
	keys := make(map[typedKey]bool)
	for _, d := range p.days {
		for key := range d.keys {
			keys[key] = true
		}
	}
	return slices.SortedFunc(maps.Keys(keys), typedKey.compare)
}

// addColumns adds the hot-table column of each key in the days' sets that
// has none yet.
func (p *promotion) addColumns(ctx context.Context, conn *sql.Conn) error {
	for _, key := range p.promotedColumns() {
		if p.existing[key.column()] {
			continue
		}
		_, err := conn.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s.%s ADD COLUMN %s %s",
			logsCatalog, hotTable(p.projectID), sqlIdentifier(key.column()), key.Type.columnType()))
		if err != nil {
			return fmt.Errorf("promote %s: %w", key.column(), err)
		}
		p.existing[key.column()] = true
	}
	return nil
}

// record adds the keys this transaction put in each day's set to the
// promoted-set table.
func (p *promotion) record(conn *sql.Conn) error {
	err := conn.Raw(func(c any) error {
		dConn, ok := c.(driver.Conn)
		if !ok {
			return fmt.Errorf("not a driver connection")
		}
		appender, err := duckdb.NewAppender(dConn, logsCatalog, "main", "log_promoted_sets")
		if err != nil {
			return err
		}
		for _, d := range p.days {
			for _, key := range d.added {
				if err := appender.AppendRow(p.projectID, d.day, string(key.Type), key.Key, key.AddedAt, key.Partway); err != nil {
					appender.Close()
					return err
				}
			}
		}
		return appender.Close()
	})
	if err != nil {
		return fmt.Errorf("record promoted keys: %w", err)
	}
	return nil
}

// sets returns the days' promoted sets as the transaction leaves them.
func (p *promotion) sets() []PromotedSet {
	var sets []PromotedSet
	for _, d := range p.days {
		if len(d.keys) > 0 {
			sets = append(sets, d.set())
		}
	}
	return sets
}

// storedSets reads a Project's promoted sets from day first to day last.
func (p *promotion) storedSets(ctx context.Context, conn *sql.Conn, first, last time.Time) (map[time.Time][]PromotedKey, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT day, type, key, added_at, partway FROM logs.log_promoted_sets
		WHERE project_id = ? AND day BETWEEN CAST(? AS DATE) AND CAST(? AS DATE)`,
		p.projectID, dayString(first), dayString(last))
	if err != nil {
		return nil, fmt.Errorf("read promoted sets: %w", err)
	}
	defer rows.Close()
	sets := make(map[time.Time][]PromotedKey)
	for rows.Next() {
		var (
			day time.Time
			key PromotedKey
		)
		if err := rows.Scan(&day, &key.Type, &key.Key, &key.AddedAt, &key.Partway); err != nil {
			return nil, err
		}
		key.AddedAt = key.AddedAt.UTC()
		sets[LogDay(day)] = append(sets[LogDay(day)], key)
	}
	return sets, rows.Err()
}

// previousSet returns the keys of the promoted set of the most recent day
// before day that has one, by type then key.
func (p *promotion) previousSet(ctx context.Context, conn *sql.Conn, day time.Time) ([]typedKey, error) {
	var stored sql.NullTime
	err := conn.QueryRowContext(ctx,
		"SELECT max(day) FROM logs.log_promoted_sets WHERE project_id = ? AND day < CAST(? AS DATE)",
		p.projectID, dayString(day)).Scan(&stored)
	if err != nil {
		return nil, fmt.Errorf("find previous promoted set: %w", err)
	}

	// A day this transaction made a set for may be more recent.
	var latest *dayPromotion
	for _, d := range p.days {
		if d.day.Before(day) && len(d.keys) > 0 && (latest == nil || d.day.After(latest.day)) {
			latest = d
		}
	}
	if latest != nil && (!stored.Valid || !LogDay(stored.Time).After(latest.day)) {
		return slices.SortedFunc(maps.Keys(latest.keys), typedKey.compare), nil
	}
	if !stored.Valid {
		return nil, nil
	}

	sets, err := p.storedSets(ctx, conn, stored.Time, stored.Time)
	if err != nil {
		return nil, err
	}
	var keys []typedKey
	for _, key := range sets[LogDay(stored.Time)] {
		keys = append(keys, typedKey{key.Type, key.Key})
	}
	slices.SortFunc(keys, typedKey.compare)
	return keys, nil
}

// rankedKeys returns the counted keys of the most recent day before day with
// counts, most popular first.
func (p *promotion) rankedKeys(ctx context.Context, conn *sql.Conn, day time.Time) ([]typedKey, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT type, key FROM logs.log_key_counts
		WHERE project_id = ? AND day = (
			SELECT max(day) FROM logs.log_key_counts WHERE project_id = ? AND day < CAST(? AS DATE)
		)
		ORDER BY logs DESC, type, key`,
		p.projectID, p.projectID, dayString(day))
	if err != nil {
		return nil, fmt.Errorf("rank keys: %w", err)
	}
	defer rows.Close()
	var keys []typedKey
	for rows.Next() {
		var key typedKey
		if err := rows.Scan(&key.Type, &key.Key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// countKeys does step 4 of Log processing inside conn's open transaction: it
// adds, per day, how many of the Logs carry each promotable key to the
// Project's popularity counts.
func countKeys(ctx context.Context, conn *sql.Conn, projectID uint32, logs []models.Log) error {
	byDay := make(map[time.Time]map[typedKey]int64)
	for _, log := range logs {
		day := LogDay(log.Time)
		if byDay[day] == nil {
			byDay[day] = make(map[typedKey]int64)
		}
		for _, key := range logKeys(log) {
			byDay[day][key]++
		}
	}

	for _, day := range slices.SortedFunc(maps.Keys(byDay), time.Time.Compare) {
		if len(byDay[day]) == 0 {
			continue
		}
		counts, err := keyCounts(ctx, conn, projectID, day)
		if err != nil {
			return err
		}
		changed, evicted := mergeKeyCounts(counts, byDay[day], keyCountsKept)
		if err := writeKeyCounts(conn, projectID, day, counts, changed, evicted); err != nil {
			return err
		}
	}
	return nil
}

// keyCounts reads a Project's popularity counts for a day.
func keyCounts(ctx context.Context, conn *sql.Conn, projectID uint32, day time.Time) (map[typedKey]int64, error) {
	rows, err := conn.QueryContext(ctx,
		"SELECT type, key, logs FROM logs.log_key_counts WHERE project_id = ? AND day = CAST(? AS DATE)",
		projectID, dayString(day))
	if err != nil {
		return nil, fmt.Errorf("read key counts: %w", err)
	}
	defer rows.Close()
	counts := make(map[typedKey]int64)
	for rows.Next() {
		var (
			key  typedKey
			logs int64
		)
		if err := rows.Scan(&key.Type, &key.Key, &logs); err != nil {
			return nil, err
		}
		counts[key] = logs
	}
	return counts, rows.Err()
}

// mergeKeyCounts adds a batch's counts to a day's counts, then keeps only the
// keep most popular keys. It is the Space-Saving top-K: a key new to a full
// top-K starts from the lowest count kept, so a key that keeps coming climbs
// past keys seen once in a burst rather than being evicted each time. It
// returns the keys whose count changed and those evicted.
func mergeKeyCounts(counts, batch map[typedKey]int64, keep int) (changed, evicted []typedKey) {
	var floor int64
	if len(counts) >= keep {
		floor = slices.Min(slices.Collect(maps.Values(counts)))
	}
	for key, n := range batch {
		if current, ok := counts[key]; ok {
			counts[key] = current + n
		} else {
			counts[key] = floor + n
		}
	}

	if len(counts) > keep {
		ranked := slices.SortedFunc(maps.Keys(counts), func(a, b typedKey) int {
			return cmp.Or(cmp.Compare(counts[b], counts[a]), a.compare(b))
		})
		evicted = ranked[keep:]
		for _, key := range evicted {
			delete(counts, key)
		}
	}
	for key := range batch {
		if _, ok := counts[key]; ok {
			changed = append(changed, key)
		}
	}
	return changed, evicted
}

// writeKeyCounts stores the counts of the changed keys of a Project's day,
// and deletes those of the evicted keys, in one MERGE fed by an appender.
func writeKeyCounts(conn *sql.Conn, projectID uint32, day time.Time, counts map[typedKey]int64, changed, evicted []typedKey) error {
	// An evicted key is fed with no Logs.
	merge := fmt.Sprintf(`
		MERGE INTO logs.log_key_counts AS c
		USING appended_data AS v
		ON c.project_id = %[1]d AND c.day = DATE %[2]s AND c.type = v.type AND c.key = v.key
		WHEN MATCHED AND v.logs = 0 THEN DELETE
		WHEN MATCHED THEN UPDATE SET logs = v.logs
		WHEN NOT MATCHED AND v.logs > 0 THEN INSERT VALUES (%[1]d, DATE %[2]s, v.type, v.key, v.logs)`,
		projectID, sqlString(dayString(day)))
	varchar, err := duckdb.NewTypeInfo(duckdb.TYPE_VARCHAR)
	if err != nil {
		return err
	}
	bigint, err := duckdb.NewTypeInfo(duckdb.TYPE_BIGINT)
	if err != nil {
		return err
	}

	err = conn.Raw(func(c any) error {
		dConn, ok := c.(driver.Conn)
		if !ok {
			return fmt.Errorf("not a driver connection")
		}
		appender, err := duckdb.NewQueryAppender(dConn, merge, "appended_data",
			[]duckdb.TypeInfo{varchar, varchar, bigint}, []string{"type", "key", "logs"})
		if err != nil {
			return err
		}
		for _, key := range changed {
			if err := appender.AppendRow(string(key.Type), key.Key, counts[key]); err != nil {
				appender.Close()
				return err
			}
		}
		for _, key := range evicted {
			if err := appender.AppendRow(string(key.Type), key.Key, int64(0)); err != nil {
				appender.Close()
				return err
			}
		}
		return appender.Close()
	})
	if err != nil {
		return fmt.Errorf("write key counts: %w", err)
	}
	return nil
}

// tableColumns returns the names of a logs.duckdb table's columns, in order.
func tableColumns(ctx context.Context, conn *sql.Conn, table string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s.%s LIMIT 0", logsCatalog, table))
	if err != nil {
		return nil, fmt.Errorf("read columns of %s: %w", table, err)
	}
	defer rows.Close()
	return rows.Columns()
}

// promotedColumnKey returns the (type, key) pair a promoted column holds, if
// column is the name of one.
func promotedColumnKey(column string) (typedKey, bool) {
	for _, t := range promotedTypes {
		if key, ok := strings.CutPrefix(column, t.Map()+"."); ok {
			return typedKey{t, key}, true
		}
	}
	return typedKey{}, false
}

// dayString is a day as a DuckDB DATE literal.
func dayString(day time.Time) string {
	return day.UTC().Format(time.DateOnly)
}

// sqlIdentifier quotes s as a SQL identifier.
func sqlIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
