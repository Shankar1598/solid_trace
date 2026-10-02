package logslayouts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDuckLakeProbe records how DuckLake (SQLite catalog) handles what ADR
// 0004's Parquet layout needs: files that promote different keys, per-Project
// retention, and moving files to another location later. It logs what it
// sees; run with LOGSBENCH_PROBE=1 -v.
func TestDuckLakeProbe(t *testing.T) {
	if os.Getenv("LOGSBENCH_PROBE") == "" {
		t.Skip("set LOGSBENCH_PROBE=1 to run")
	}
	g := NewGenerator(1)
	l := mapLayout{}
	db := writeTestLogs(t, l, g)
	dir := t.TempDir()
	meta := filepath.Join(dir, "meta.sqlite")
	data := filepath.Join(dir, "data") + "/"

	ex := func(q string) error {
		_, err := db.Exec(q)
		if err != nil {
			t.Logf("ERROR %s\n    %s", firstLine(err), q)
		}
		return err
	}
	show := func(title, q string) [][]string {
		rows, err := queryStrings(t.Context(), db, q, 0)
		if err != nil {
			t.Logf("ERROR %s: %s\n    %s", title, firstLine(err), q)
			return nil
		}
		t.Logf("%s:", title)
		for _, r := range rows {
			t.Logf("    %s", strings.Join(r, " | "))
		}
		return rows
	}
	mustEx := func(q string) {
		if ex(q) != nil {
			t.FailNow()
		}
	}
	for _, q := range []string{"INSTALL ducklake", "INSTALL sqlite", "LOAD ducklake", "LOAD sqlite"} {
		mustEx(q)
	}
	attach := func(opts string) error {
		return ex(fmt.Sprintf("ATTACH 'ducklake:sqlite:%s' AS lake %s", meta, opts))
	}
	if attach(fmt.Sprintf("(DATA_PATH %s)", sqlString(data))) != nil {
		t.FailNow()
	}
	const md = "__ducklake_metadata_lake"

	// 1. One table, partitioned by Project and hour; two exports that promote
	// different keys. A key is promoted by adding a column; INSERT BY NAME
	// leaves columns the batch does not promote NULL.
	mustEx(fmt.Sprintf("CREATE TABLE lake.logs (%s, %s)", fixedColumns, l.attrColumns()))
	ex("ALTER TABLE lake.logs SET PARTITIONED BY (project_id, year(time), month(time), day(time), hour(time))")
	batches := []struct {
		where    string
		promoted []PromotedKey
	}{
		{"id < 15000", []PromotedKey{{"attrs_string", CommonKey}, {"attrs_string", GroupKey}, {"attrs_number", "http.status_code"}}},
		{"id >= 15000", []PromotedKey{{"attrs_string", CommonKey}, {"attrs_string", "region"}, {"attrs_number", MixedKey}}},
	}
	columns := map[string]bool{}
	for _, b := range batches {
		for _, p := range b.promoted {
			if !columns[p.Column()] {
				columns[p.Column()] = true
				typ := map[string]string{"attrs_string": "VARCHAR", "attrs_number": "DOUBLE"}[p.Map]
				mustEx(fmt.Sprintf("ALTER TABLE lake.logs ADD COLUMN %s %s", quoteIdent(p.Column()), typ))
			}
		}
		mustEx(fmt.Sprintf("INSERT INTO lake.logs BY NAME SELECT %s FROM main.logs WHERE %s ORDER BY time, id",
			exportColumns(l, b.promoted), b.where))
	}
	show("data files (catalog)", "SELECT data_file_id, partition_id, record_count, path_is_relative, path FROM "+md+".ducklake_data_file ORDER BY data_file_id")
	show("partition values", "SELECT * FROM "+md+".ducklake_file_partition_value ORDER BY data_file_id, partition_key_index")
	show("Parquet columns per file on disk (promoted ones)", fmt.Sprintf(
		"SELECT regexp_replace(file_name, '.*/data/', ''), string_agg(name, ', ' ORDER BY name) FROM parquet_schema(%s) WHERE name LIKE 'attrs_%%.%%' GROUP BY ALL ORDER BY 1",
		sqlString(data+"**/*.parquet")))
	show("per-file promoted columns the catalog knows (non-null values)", `SELECT s.data_file_id, string_agg(c.column_name, ', ' ORDER BY c.column_name)
		FROM `+md+`.ducklake_file_column_stats s JOIN `+md+`.ducklake_column c USING (table_id, column_id)
		WHERE c.column_name LIKE 'attrs_%.%' AND s.null_count < s.value_count + s.null_count AND s.value_count > 0 GROUP BY ALL ORDER BY 1`)
	show("column stats columns", "SELECT column_name FROM (DESCRIBE "+md+".ducklake_file_column_stats)")

	// 2. The query set over the lake, keys read as coalesce(column, map[key]).
	p := VerifyParams(g, testLogs/2)
	hot := runAll(t, db, l, "logs", p)
	pl := promotedLayout{form: FormCoalesce, columns: columns}
	if lake := runAll(t, db, pl, "lake.logs", p); !reflect.DeepEqual(lake, hot) {
		t.Errorf("DuckLake results differ from the hot table:\n got  %v\n want %v", lake, hot)
	} else {
		t.Logf("query set over lake.logs matches the hot table")
	}
	show("EXPLAIN of a promoted-key filter", fmt.Sprintf("EXPLAIN SELECT count(*) FROM lake.logs WHERE %s = 'POST'", quoteIdent("attrs_string."+CommonKey)))

	// 3. Retention: drop a whole Project, then a partial delete, then expire
	// and clean up.
	countFiles := func() int {
		n := 0
		filepath.Walk(data, func(path string, info os.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(path, ".parquet") {
				n++
			}
			return nil
		})
		return n
	}
	t.Logf("parquet files on disk before deletes: %d", countFiles())
	ex("DELETE FROM lake.logs WHERE project_id = 3")
	show("after deleting Project 3: files ended / delete files", "SELECT (SELECT count(*) FROM "+md+".ducklake_data_file WHERE end_snapshot IS NOT NULL), (SELECT count(*) FROM "+md+".ducklake_delete_file)")
	ex("DELETE FROM lake.logs WHERE project_id = 2 AND id < 5000")
	show("after a partial delete in Project 2: files ended / delete files", "SELECT (SELECT count(*) FROM "+md+".ducklake_data_file WHERE end_snapshot IS NOT NULL), (SELECT count(*) FROM "+md+".ducklake_delete_file)")
	t.Logf("parquet files on disk after deletes: %d", countFiles())
	ex("CALL ducklake_expire_snapshots('lake', older_than => now())")
	ex("CALL ducklake_cleanup_old_files('lake', cleanup_all => true)")
	t.Logf("parquet files on disk after expire + cleanup: %d", countFiles())
	show("rows per Project now", "SELECT project_id, count(*) FROM lake.logs GROUP BY ALL ORDER BY 1")

	// 4. Moving files. Stored paths first. Checks read the files: count(*)
	// alone is answered from the catalog's record counts.
	show("ducklake_metadata", "SELECT key, value FROM "+md+".ducklake_metadata ORDER BY key")
	show("schema and table paths", "SELECT 'schema', schema_name, path, path_is_relative FROM "+md+".ducklake_schema UNION ALL SELECT 'table', table_name, path, path_is_relative FROM "+md+".ducklake_table")
	const read = "SELECT count(*), sum(length(message)) FROM lake.logs"
	show("4: rows and message bytes before moving", read)
	files := func(root string) {
		var found []string
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(path, ".parquet") {
				rel, _ := filepath.Rel(dir, path)
				found = append(found, rel)
			}
			return nil
		})
		t.Logf("parquet files under %s: %d", filepath.Base(root), len(found))
		for _, f := range found {
			t.Logf("    %s", f)
		}
	}
	mustEx("DETACH lake")

	// 4a. Tiering: keep the old files where they are and write new ones
	// elsewhere with OVERRIDE_DATA_PATH, then reattach normally.
	tier := filepath.Join(dir, "tier2") + "/"
	if attach(fmt.Sprintf("(DATA_PATH %s, OVERRIDE_DATA_PATH true)", sqlString(tier))) == nil {
		ex(fmt.Sprintf("INSERT INTO lake.logs BY NAME SELECT %s FROM main.logs WHERE id < 100", exportColumns(l, nil)))
		show("4a: new files in the catalog", "SELECT data_file_id, path_is_relative, path FROM "+md+".ducklake_data_file WHERE end_snapshot IS NULL AND data_file_id > 5 ORDER BY data_file_id")
		show("4a: read in the overriding session", read)
		ex("DETACH lake")
	}
	files(tier)
	if attach("") == nil {
		show("4a: read after reattaching with the catalog's data path", read)
		ex("DETACH lake")
	}

	// 4b. Move everything: copy the files (standing in for an upload to a
	// bucket), delete the originals, then point the catalog at the copy.
	moved := filepath.Join(dir, "moved") + "/"
	for _, src := range []string{data, tier} {
		if out, err := exec.Command("cp", "-r", src+".", moved).CombinedOutput(); err != nil {
			os.MkdirAll(moved, 0o755)
			if out, err = exec.Command("cp", "-r", src+".", moved).CombinedOutput(); err != nil {
				t.Fatalf("cp: %v %s", err, out)
			}
		}
	}
	os.RemoveAll(data)
	os.RemoveAll(tier)
	attach(fmt.Sprintf("(DATA_PATH %s)", sqlString(moved)))
	if attach(fmt.Sprintf("(DATA_PATH %s, OVERRIDE_DATA_PATH true)", sqlString(moved))) == nil {
		show("4b: read with OVERRIDE_DATA_PATH set to the new location", read)
		ex("DETACH lake")
	}
	if ex(fmt.Sprintf("ATTACH %s AS cat (TYPE sqlite)", sqlString(meta))) == nil {
		ex(fmt.Sprintf("UPDATE cat.ducklake_metadata SET value = %s WHERE key = 'data_path'", sqlString(moved)))
		ex("DETACH cat")
	}
	if attach("") == nil {
		show("4b: read after editing data_path in the catalog", read)
		show("4b: data_path now", "SELECT value FROM "+md+".ducklake_metadata WHERE key = 'data_path'")
	}

	// 4c. Register a file written elsewhere (as if already uploaded).
	ext := filepath.Join(dir, "external", "data.parquet")
	os.MkdirAll(filepath.Dir(ext), 0o755)
	if ex(fmt.Sprintf("COPY (SELECT %s FROM main.logs WHERE id < 50) TO %s (FORMAT parquet)", exportColumns(l, batches[0].promoted), sqlString(ext))) == nil {
		if ex(fmt.Sprintf("CALL ducklake_add_data_files('lake', 'logs', %s)", sqlString(ext))) != nil {
			ex(fmt.Sprintf("CALL ducklake_add_data_files('lake', 'logs', %s, allow_missing => true)", sqlString(ext)))
		}
		show("4c: registered file", "SELECT data_file_id, path_is_relative, path FROM "+md+".ducklake_data_file WHERE path LIKE '%external%'")
		show("4c: read", read)
	}

	// 4d. Move one old file elsewhere and record its absolute path in the
	// catalog, leaving the rest under data_path.
	ex("DETACH lake")
	var rel string
	filepath.Walk(moved, func(path string, info os.FileInfo, err error) error {
		if rel == "" && err == nil && strings.Contains(path, "project_id=1") && strings.HasSuffix(path, ".parquet") {
			rel = path
		}
		return nil
	})
	away := filepath.Join(dir, "bucket", filepath.Base(rel))
	os.MkdirAll(filepath.Dir(away), 0o755)
	if err := os.Rename(rel, away); err != nil {
		t.Fatal(err)
	}
	if ex(fmt.Sprintf("ATTACH %s AS cat (TYPE sqlite)", sqlString(meta))) == nil {
		ex(fmt.Sprintf("UPDATE cat.ducklake_data_file SET path = %s, path_is_relative = false WHERE path LIKE %s",
			sqlString(away), sqlString("%"+filepath.Base(rel))))
		ex("DETACH cat")
	}
	if attach("") == nil {
		show("4d: read with one file moved and its absolute path in the catalog", read)
	}
}
