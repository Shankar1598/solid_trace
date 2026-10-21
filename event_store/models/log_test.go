package models

import (
	"encoding/json"
	"math"
	"testing"
)

func TestAttributesAreOneJSONObjectInNameOrder(t *testing.T) {
	attributes := Attributes{
		String: map[string]string{"b": "<b>&</b>", "a\u2028": "bad \xff byte"},
		Number: map[string]float64{"n.zero": math.Copysign(0, -1), "n.big": 1e21, "n.small": 1e-7, "n.whole": 200},
		Bool:   map[string]bool{"cached": true},
		JSON:   map[string]string{"tags": `["<a>",1.50,{"z":1,"a":2}]`},
	}
	got, err := json.Marshal(attributes)
	if err != nil {
		t.Fatal(err)
	}
	// As encoding/json writes a map: names sorted, HTML escaped, an invalid
	// byte replaced, and JSON values kept as they are.
	want := `{"a\u2028":"bad ` + "\uFFFD" + ` byte","b":"\u003cb\u003e\u0026\u003c/b\u003e","cached":true,` +
		`"n.big":1e+21,"n.small":1e-7,"n.whole":200,"n.zero":-0,"tags":["\u003ca\u003e",1.50,{"z":1,"a":2}]}`
	if string(got) != want {
		t.Errorf("Expected\n%s\ngot\n%s", want, got)
	}

	if got, _ := json.Marshal(Attributes{}); string(got) != "{}" {
		t.Errorf("Expected no Attributes to be {}, got %s", got)
	}
}
