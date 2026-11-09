package routers

import (
	"encoding/json"
	"strings"
	"testing"
)

func fp(v float64) *float64 { return &v }

func TestNormalizeConditions_AllKinds(t *testing.T) {
	cases := []struct {
		name string
		in   rawSignalCondition
		want string
	}{
		{"vol_ma", rawSignalCondition{ID: "a", Side: "long", Kind: "vol_ma", Period: fp(5), Op: "gt", Value: fp(1.5), Entity: "foreign", Cross: "golden"},
			`{"id":"a","side":"long","kind":"vol_ma","op":"gt","value":1.5,"period":5}`},
		{"insti_avg", rawSignalCondition{ID: "a", Side: "short", Kind: "insti_avg", Entity: "trust", Period: fp(10), Op: "lt", Value: fp(-100), Cross: "x"},
			`{"id":"a","side":"short","kind":"insti_avg","op":"lt","value":-100,"period":10,"entity":"trust"}`},
		{"margin_balance", rawSignalCondition{ID: "a", Side: "long", Kind: "margin_balance", Op: "gt", Value: fp(10), Period: fp(5)},
			`{"id":"a","side":"long","kind":"margin_balance","op":"gt","value":10}`},
		{"short_balance", rawSignalCondition{ID: "a", Side: "long", Kind: "short_balance", Op: "gt", Value: fp(10)},
			`{"id":"a","side":"long","kind":"short_balance","op":"gt","value":10}`},
		{"short_margin_ratio", rawSignalCondition{ID: "a", Side: "long", Kind: "short_margin_ratio", Op: "lt", Value: fp(20)},
			`{"id":"a","side":"long","kind":"short_margin_ratio","op":"lt","value":20}`},
		{"macd_hist", rawSignalCondition{ID: "a", Side: "long", Kind: "macd_hist", Op: "gt", Value: fp(0)},
			`{"id":"a","side":"long","kind":"macd_hist","op":"gt","value":0}`},
		{"bb_percent_b", rawSignalCondition{ID: "a", Side: "long", Kind: "bb_percent_b", Op: "lt", Value: fp(0.2)},
			`{"id":"a","side":"long","kind":"bb_percent_b","op":"lt","value":0.2}`},
		{"kd_k", rawSignalCondition{ID: "a", Side: "long", Kind: "kd_k", Op: "lt", Value: fp(20)},
			`{"id":"a","side":"long","kind":"kd_k","op":"lt","value":20}`},
		{"rsi", rawSignalCondition{ID: "a", Side: "long", Kind: "rsi", Period: fp(9), Op: "gt", Value: fp(50), Entity: "foreign"},
			`{"id":"a","side":"long","kind":"rsi","op":"gt","value":50,"period":9}`},
		{"kd_cross", rawSignalCondition{ID: "a", Side: "long", Kind: "kd_cross", Cross: "golden", Op: "gt", Value: fp(1), Period: fp(5)},
			`{"id":"a","side":"long","kind":"kd_cross","cross":"golden"}`},
		{"price_ma", rawSignalCondition{ID: "a", Side: "long", Kind: "price_ma", Period: fp(20), Op: "gt", Value: fp(3)},
			`{"id":"a","side":"long","kind":"price_ma","op":"gt","period":20}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := normalizeConditions([]rawSignalCondition{c.in})
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(out[0])
			if string(b) != c.want {
				t.Errorf("got %s want %s", b, c.want)
			}
		})
	}
}

func TestNormalizeConditions_KeepsZeroValue(t *testing.T) {
	out, err := normalizeConditions([]rawSignalCondition{{ID: "a", Side: "long", Kind: "macd_hist", Op: "gt", Value: fp(0)}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Value == nil || *out[0].Value != 0 {
		t.Fatal("zero value lost")
	}
	b, _ := json.Marshal(out[0])
	if !strings.Contains(string(b), `"value":0`) {
		t.Errorf("got %s", b)
	}
}

func TestNormalizeConditions_NonIntegerPeriod(t *testing.T) {
	_, err := normalizeConditions([]rawSignalCondition{{ID: "a", Side: "long", Kind: "rsi", Period: fp(9.5), Op: "gt", Value: fp(1)}})
	if err == nil || err.Error() != "invalid conditions[0].period" {
		t.Errorf("got %v", err)
	}
}

func TestNormalizeConditions_Errors(t *testing.T) {
	ok := func(mod func(*rawSignalCondition)) rawSignalCondition {
		c := rawSignalCondition{ID: "a", Side: "long", Kind: "rsi", Period: fp(9), Op: "gt", Value: fp(50)}
		mod(&c)
		return c
	}
	cases := []struct {
		name string
		in   []rawSignalCondition
		want string
	}{
		{"id empty", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.ID = "" })}, "invalid conditions[0].id"},
		{"id 65", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.ID = strings.Repeat("x", 65) })}, "invalid conditions[0].id"},
		{"id NUL", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.ID = "a\u0000b" })}, "invalid conditions[0].id"},
		{"id newline", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.ID = "a\nb" })}, "invalid conditions[0].id"},
		{"id dup", []rawSignalCondition{ok(func(c *rawSignalCondition) {}), ok(func(c *rawSignalCondition) {})}, "invalid conditions[1].id"},
		{"side", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Side = "both" })}, "invalid conditions[0].side"},
		{"kind", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Kind = "abc" })}, "invalid conditions[0].kind"},
		{"entity", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Kind = "insti_avg"; c.Entity = "dealer"; c.Period = fp(5) })}, "invalid conditions[0].entity"},
		{"rsi period", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Period = fp(7) })}, "invalid conditions[0].period"},
		{"vol_ma no period", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Kind = "vol_ma"; c.Period = nil })}, "invalid conditions[0].period"},
		{"insti period", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Kind = "insti_avg"; c.Entity = "foreign"; c.Period = fp(25) })}, "invalid conditions[0].period"},
		{"price_ma period", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Kind = "price_ma"; c.Period = fp(60) })}, "invalid conditions[0].period"},
		{"op eq", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Op = "eq" })}, "invalid conditions[0].op"},
		{"price_ma no op", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Kind = "price_ma"; c.Period = fp(5); c.Op = "" })}, "invalid conditions[0].op"},
		{"kd_k no value", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Kind = "kd_k"; c.Period = nil; c.Value = nil })}, "invalid conditions[0].value"},
		{"cross", []rawSignalCondition{ok(func(c *rawSignalCondition) { c.Kind = "kd_cross"; c.Cross = "up" })}, "invalid conditions[0].cross"},
		{"third", []rawSignalCondition{
			ok(func(c *rawSignalCondition) { c.ID = "1" }),
			ok(func(c *rawSignalCondition) { c.ID = "2" }),
			ok(func(c *rawSignalCondition) { c.ID = "3"; c.Op = "eq" }),
		}, "invalid conditions[2].op"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := normalizeConditions(c.in)
			if err == nil || err.Error() != c.want {
				t.Errorf("got %v want %s", err, c.want)
			}
		})
	}
}

func TestNormalizeConditions_Empty(t *testing.T) {
	for _, in := range [][]rawSignalCondition{nil, {}} {
		out, err := normalizeConditions(in)
		if err != nil || out == nil || len(out) != 0 {
			t.Fatalf("out=%v err=%v", out, err)
		}
		b, _ := json.Marshal(out)
		if string(b) != "[]" {
			t.Errorf("got %s", b)
		}
	}
}

func TestCountBySide(t *testing.T) {
	l, s := countBySide([]signalCondition{{Side: "long"}, {Side: "short"}, {Side: "long"}})
	if l != 2 || s != 1 {
		t.Errorf("got %d,%d", l, s)
	}
}
