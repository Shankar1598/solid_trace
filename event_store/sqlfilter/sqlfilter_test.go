package sqlfilter

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestScanFindsEachFilterOnceInOrderOfFirstUse(t *testing.T) {
	sql := `SELECT count() FROM logs
		WHERE (empty({region:Array(String)}) OR region IN {region:Array(String)})
		AND ({ q : String } = '' OR positionCaseInsensitive(message, {q:String}) > 0)
		AND ({env:Nullable(String)} IS NULL OR env = {env:Nullable(String)})`
	filters, err := Scan(sql)
	if err != nil {
		t.Fatal(err)
	}
	want := []Filter{{"region", Array}, {"q", Text}, {"env", Nullable}}
	if !slices.Equal(filters, want) {
		t.Errorf("want %v, got %v", want, filters)
	}
}

func TestScanSkipsStringsQuotedNamesHeredocsAndComments(t *testing.T) {
	sql := `SELECT '{a:String}', 'it''s {b:String}', 'back\' {c:String}', "{d:String}", ` + "`{e:String}`" + `,
		$$ {f:String} $$, $tag$ {g:String} $tag$, map('k', 1) AS m -- {h:String}
		# {i:String}
		/* {j:String} /* {k:String} */ {l:String} */ FROM logs WHERE x = {real:String}`
	filters, err := Scan(sql)
	if err != nil {
		t.Fatal(err)
	}
	if got := Names(filters); !slices.Equal(got, []string{"real"}) {
		t.Errorf("want only real, got %v", got)
	}
}

func TestScanFindsNoFilterInAMapLiteralOrBraces(t *testing.T) {
	filters, err := Scan(`SELECT {'a': 1}, {1:2}, '{}' FROM logs`)
	if err != nil || len(filters) != 0 {
		t.Errorf("want no filters, got %v, %v", filters, err)
	}
}

func TestScanRefusesAnotherTypeAndANameWithTwoTypes(t *testing.T) {
	for sql, want := range map[string]string{
		"SELECT {n:UInt32}":                        "{n:UInt32}: a Dashboard filter's type must be Array(String), String or Nullable(String)",
		"SELECT {n:LowCardinality(String)}":        "{n:LowCardinality(String)}: a Dashboard filter's type must be",
		"SELECT {n:string}":                        "{n:string}: a Dashboard filter's type must be",
		"SELECT {n:String}, {n:Array(String)}":     "n is named as String and as Array(String)",
		"SELECT {n:String}, {n: Nullable(String)}": "n is named as String and as Nullable(String)",
	} {
		_, err := Scan(sql)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want an error with %q, got %v", sql, want, err)
		}
	}
}

func TestParametersWriteEachTypeAndAll(t *testing.T) {
	filters := []Filter{{"region", Array}, {"q", Text}, {"env", Nullable}}
	cases := []struct {
		values map[string][]string
		want   map[string]string
	}{
		{nil, map[string]string{"region": "[]", "q": "", "env": `\N`}},
		{map[string][]string{"region": {}, "q": {}, "env": {}}, map[string]string{"region": "[]", "q": "", "env": `\N`}},
		{
			map[string][]string{"region": {"r1", "o'brien", `a\b`}, "q": {"time", "out"}, "env": {"prod"}, "other": {"x"}},
			map[string]string{"region": `['r1','o\'brien','a\\b']`, "q": "time out", "env": "prod"},
		},
		{
			map[string][]string{"q": {"a\tb\nc\\d"}, "env": {`\N`}},
			map[string]string{"region": "[]", "q": `a\tb\nc\\d`, "env": `\\N`},
		},
	}
	for _, c := range cases {
		if got := Parameters(filters, c.values); !maps.Equal(got, c.want) {
			t.Errorf("%v: want %v, got %v", c.values, c.want, got)
		}
	}
}
