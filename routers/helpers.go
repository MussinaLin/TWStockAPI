package routers

import (
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// rowsToMaps converts pgx rows to a slice of maps, handling type conversions
// for JSON serialization (dates → ISO strings, numeric → float64).
func rowsToMaps(rows pgx.Rows) ([]map[string]any, error) {
	descs := rows.FieldDescriptions()
	cols := make([]string, len(descs))
	for i, d := range descs {
		cols[i] = d.Name
	}

	var result []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, col := range cols {
			m[col] = convertValue(values[i])
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func convertValue(v any) any {
	switch val := v.(type) {
	case time.Time:
		return val.Format(time.DateOnly)
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return nil
		}
		return val
	case float32:
		f := float64(val)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f
	case pgtype.Numeric:
		f, err := val.Float64Value()
		if err != nil || !f.Valid {
			return nil
		}
		if math.IsNaN(f.Float64) || math.IsInf(f.Float64, 0) {
			return nil
		}
		return f.Float64
	default:
		return v
	}
}
