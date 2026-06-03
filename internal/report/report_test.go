package report

import (
	"strings"
	"testing"
)

func TestCompileRejectsUnknownFields(t *testing.T) {
	cases := []struct {
		name string
		def  Definition
	}{
		{"unknown entity", Definition{Entity: "secrets", Measures: []Measure{{Agg: "count"}}}},
		{"no measures", Definition{Entity: "orders"}},
		{"unknown dimension", Definition{Entity: "orders", Dimensions: []string{"ssn"}, Measures: []Measure{{Agg: "count"}}}},
		{"unknown measure field", Definition{Entity: "orders", Measures: []Measure{{Field: "password", Agg: "sum"}}}},
		{"bad aggregation", Definition{Entity: "orders", Measures: []Measure{{Field: "grand_total", Agg: "exec"}}}},
		{"unknown filter field", Definition{Entity: "orders", Measures: []Measure{{Agg: "count"}}, Filters: []Filter{{Field: "secret", Op: "eq", Value: "x"}}}},
		{"bad filter op", Definition{Entity: "orders", Measures: []Measure{{Agg: "count"}}, Filters: []Filter{{Field: "status", Op: "; DROP", Value: "x"}}}},
	}
	for _, c := range cases {
		if _, err := Compile(1, c.def); err == nil {
			t.Errorf("%s: expected compile error, got none", c.name)
		}
	}
}

func TestCompileShape(t *testing.T) {
	def := Definition{
		Entity:     "orders",
		Dimensions: []string{"month"},
		Measures:   []Measure{{Field: "grand_total", Agg: "sum"}, {Agg: "count"}},
		Filters:    []Filter{{Field: "status", Op: "eq", Value: "completed"}},
	}
	c, err := Compile(7, def)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// org is always $1; the filter value is parameterized ($2), never inlined.
	if len(c.Args) != 2 || c.Args[0].(int64) != 7 || c.Args[1].(string) != "completed" {
		t.Errorf("args = %#v", c.Args)
	}
	if strings.Contains(c.SQL, "completed") {
		t.Error("filter value must be a bound parameter, not inlined")
	}
	for _, want := range []string{"FROM orders o", "GROUP BY", "WHERE o.organization_id = $1", "o.status = $2", "LIMIT"} {
		if !strings.Contains(c.SQL, want) {
			t.Errorf("SQL missing %q:\n%s", want, c.SQL)
		}
	}
	if len(c.Cols) != 3 || c.Cols[0] != "month" || c.Cols[1] != "grand_total_sum" || c.Cols[2] != "count" {
		t.Errorf("cols = %v", c.Cols)
	}
}

func TestCompileNumericFilterCoercesToInt(t *testing.T) {
	// customer_id is a bigint column; a JSON number arrives as float64 and must
	// be passed as int64 so Postgres doesn't choke on bigint = double precision.
	def := Definition{
		Entity:   "orders",
		Measures: []Measure{{Agg: "count"}},
		Filters:  []Filter{{Field: "customer", Op: "eq", Value: float64(42)}},
	}
	c, err := Compile(1, def)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, ok := c.Args[1].(int64); !ok {
		t.Errorf("customer filter arg type = %T, want int64", c.Args[1])
	}
}

func TestSchemaListsAllowList(t *testing.T) {
	s := Schema()
	o, ok := s["orders"]
	if !ok {
		t.Fatal("orders entity missing from schema")
	}
	if !contains(o.Measures, "count") || !contains(o.Measures, "grand_total") {
		t.Errorf("orders measures = %v", o.Measures)
	}
	if !contains(o.Dimensions, "month") {
		t.Errorf("orders dimensions = %v", o.Dimensions)
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
