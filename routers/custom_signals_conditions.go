package routers

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 前端送來的單一條件；未知欄位由 encoding/json 忽略
type rawSignalCondition struct {
	ID     string   `json:"id"`
	Side   string   `json:"side"`
	Kind   string   `json:"kind"`
	Op     string   `json:"op"`
	Value  *float64 `json:"value"`
	Period *float64 `json:"period"` // 用 float 接，才能分辨 9.5 並指出欄位
	Entity string   `json:"entity"`
	Cross  string   `json:"cross"`
}

// 存進 DB / 回給前端的條件：只帶該 kind 用得到的欄位
type signalCondition struct {
	ID     string   `json:"id"`
	Side   string   `json:"side"`
	Kind   string   `json:"kind"`
	Op     string   `json:"op,omitempty"`
	Value  *float64 `json:"value,omitempty"`
	Period *int     `json:"period,omitempty"`
	Entity string   `json:"entity,omitempty"`
	Cross  string   `json:"cross,omitempty"`
}

type conditionError struct {
	Index int
	Field string
}

func (e *conditionError) Error() string {
	return fmt.Sprintf("invalid conditions[%d].%s", e.Index, e.Field)
}

type kindSpec struct {
	entity  bool
	periods []int // 空表示該 kind 無 period
	op      bool
	value   bool
	cross   bool
}

var (
	periods3  = []int{5, 10, 20}
	kindSpecs = map[string]kindSpec{
		"vol_ma":             {periods: periods3, op: true, value: true},
		"insti_avg":          {entity: true, periods: []int{5, 10, 15, 20, 30}, op: true, value: true},
		"margin_balance":     {op: true, value: true},
		"short_balance":      {op: true, value: true},
		"short_margin_ratio": {op: true, value: true},
		"macd_hist":          {op: true, value: true},
		"bb_percent_b":       {op: true, value: true},
		"kd_k":               {op: true, value: true},
		"rsi":                {periods: []int{9, 14}, op: true, value: true},
		"kd_cross":           {cross: true},
		"price_ma":           {periods: periods3, op: true},
	}
)

func normalizeConditions(in []rawSignalCondition) ([]signalCondition, error) {
	// 不依輸入長度預先配置，避免惡意的超長陣列放大記憶體用量。
	hint := min(len(in), 64)
	out := make([]signalCondition, 0, hint)
	seen := make(map[string]bool, hint)
	for i, r := range in {
		fail := func(field string) ([]signalCondition, error) {
			return nil, &conditionError{Index: i, Field: field}
		}
		if n := utf8.RuneCountInString(r.ID); n < 1 || n > 64 || seen[r.ID] || strings.ContainsFunc(r.ID, unicode.IsControl) {
			return fail("id")
		}
		seen[r.ID] = true
		if r.Side != "long" && r.Side != "short" {
			return fail("side")
		}
		spec, ok := kindSpecs[r.Kind]
		if !ok {
			return fail("kind")
		}
		c := signalCondition{ID: r.ID, Side: r.Side, Kind: r.Kind}
		if spec.entity {
			if r.Entity != "foreign" && r.Entity != "trust" {
				return fail("entity")
			}
			c.Entity = r.Entity
		}
		if len(spec.periods) > 0 {
			if r.Period == nil || *r.Period != math.Trunc(*r.Period) {
				return fail("period")
			}
			p := int(*r.Period)
			if !containsInt(spec.periods, p) {
				return fail("period")
			}
			c.Period = &p
		}
		if spec.op {
			if r.Op != "gt" && r.Op != "lt" {
				return fail("op")
			}
			c.Op = r.Op
		}
		if spec.value {
			if r.Value == nil {
				return fail("value")
			}
			v := *r.Value
			c.Value = &v
		}
		if spec.cross {
			if r.Cross != "golden" && r.Cross != "death" {
				return fail("cross")
			}
			c.Cross = r.Cross
		}
		out = append(out, c)
	}
	return out, nil
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func countBySide(conds []signalCondition) (long, short int) {
	for _, c := range conds {
		switch c.Side {
		case "long":
			long++
		case "short":
			short++
		}
	}
	return
}
