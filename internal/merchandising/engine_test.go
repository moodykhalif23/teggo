package merchandising

import (
	"reflect"
	"testing"
)

func TestExpandQuery(t *testing.T) {
	syn := map[string]string{"tee": "t-shirt, tshirt", "valve": "spigot"}
	cases := []struct{ q, want string }{
		{"tee", "tee OR t-shirt OR tshirt"},
		{"  Valve ", "Valve OR spigot"}, // trimmed + case-insensitive match, original casing kept
		{"widget", "widget"},            // no synonym
		{"", ""},
	}
	for _, c := range cases {
		if got := ExpandQuery(c.q, syn); got != c.want {
			t.Errorf("ExpandQuery(%q): want %q, got %q", c.q, c.want, got)
		}
	}
}

func TestReorder(t *testing.T) {
	ids := []int64{1, 2, 3, 4, 5}

	// Pin 5 then 4 (by position), boost 3, bury 1.
	rules := []Rule{
		{ProductID: 5, Action: "pin", Position: 0},
		{ProductID: 4, Action: "pin", Position: 1},
		{ProductID: 3, Action: "boost"},
		{ProductID: 1, Action: "bury"},
	}
	got := Reorder(ids, rules)
	want := []int64{5, 4, 3, 2, 1} // pinned(5,4) → boosted(3) → normal(2) → buried(1)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Reorder: want %v, got %v", want, got)
	}

	// Rules referencing absent products are ignored.
	if got := Reorder([]int64{1, 2}, []Rule{{ProductID: 99, Action: "pin"}}); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Errorf("absent rule should be a no-op, got %v", got)
	}

	// No rules → unchanged.
	if got := Reorder(ids, nil); !reflect.DeepEqual(got, ids) {
		t.Errorf("no rules: want unchanged, got %v", got)
	}
}
