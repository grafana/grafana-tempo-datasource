package tempo

import (
	"testing"

	"github.com/stretchr/testify/require"

	schemas "github.com/grafana/schemads"
)

func TestTraceQLFromSchemadsFilters(t *testing.T) {
	eq := func(name string, value any) []schemas.ColumnFilter {
		return []schemas.ColumnFilter{{
			Name: name,
			Conditions: []schemas.FilterCondition{{
				Operator: schemas.OperatorEquals,
				Value:    value,
			}},
		}}
	}

	tests := []struct {
		name    string
		filters []schemas.ColumnFilter
		want    string
	}{
		{name: "empty", filters: nil, want: "{}"},
		{
			name: "span scope and numeric status",
			filters: []schemas.ColumnFilter{
				{Name: "span.db", Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorEquals, Value: "postgres",
				}}},
				{Name: "status", Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorEquals, Value: float64(200),
				}}},
			},
			want: `{span.db="postgres" && status=200}`,
		},
		{name: "span:status enum", filters: eq("span:status", "error"), want: `{span:status=error}`},
		{
			name: "status IN enum",
			filters: []schemas.ColumnFilter{{
				Name: "status",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorIn,
					Values:   []any{"ok", "error"},
				}},
			}},
			want: `{(status=ok || status=error)}`,
		},
		{
			name: "status not equals enum",
			filters: []schemas.ColumnFilter{{
				Name: "status",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorNotEquals,
					Value:    "ok",
				}},
			}},
			want: `{status!=ok}`,
		},
		{name: "unknown status stays quoted", filters: eq("status", "custom"), want: `{status="custom"}`},
		{name: "string intrinsic quoted", filters: eq("name", "GET /api"), want: `{name="GET /api"}`},
		{name: "non-enum attribute quoted", filters: eq("span.db", "postgres"), want: `{span.db="postgres"}`},
		{
			name: "LIKE anchors full string",
			filters: []schemas.ColumnFilter{{
				Name: "name",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorLike,
					Value:    "foo",
				}},
			}},
			want: `{name=~"^foo$"}`,
		},
		{
			name: "LIKE wildcard",
			filters: []schemas.ColumnFilter{{
				Name: "name",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorLike,
					Value:    "%foo%",
				}},
			}},
			want: `{name=~"^.*foo.*$"}`,
		},
	}

	for _, status := range []string{"ok", "error", "unset"} {
		tests = append(tests, struct {
			name    string
			filters []schemas.ColumnFilter
			want    string
		}{
			name: "status enum " + status,
			filters: eq("status", status),
			want:    "{status=" + status + "}",
		})
	}
	for _, kind := range []string{"unspecified", "internal", "server", "client", "producer", "consumer"} {
		tests = append(tests, struct {
			name    string
			filters []schemas.ColumnFilter
			want    string
		}{
			name: "kind enum " + kind,
			filters: eq("kind", kind),
			want:    "{kind=" + kind + "}",
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := traceQLFromSchemadsFilters(tt.filters)
			require.NoError(t, err)
			require.Equal(t, tt.want, q)
		})
	}
}

func TestTraceQLFromSchemadsFilters_Errors(t *testing.T) {
	tests := []struct {
		name      string
		filters   []schemas.ColumnFilter
		errSubstr []string
	}{
		{
			name: "time column rejected",
			filters: []schemas.ColumnFilter{{
				Name: tempoSpanColTime,
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorGreaterThan,
					Value:    "2024-01-01",
				}},
			}},
			errSubstr: []string{"filtering on column", tempoSpanColTime},
		},
		{
			name: "multi-value unsupported type",
			filters: []schemas.ColumnFilter{{
				Name: "name",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorEquals,
					Values:   []any{"a", map[string]any{"x": 1}},
				}},
			}},
			errSubstr: []string{"converting multi-value match operand", "unsupported value type"},
		},
		{
			name: "multi-value null operand",
			filters: []schemas.ColumnFilter{{
				Name: "name",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorEquals,
					Values:   []any{"a", nil},
				}},
			}},
			errSubstr: []string{"converting multi-value match operand", "value is null"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := traceQLFromSchemadsFilters(tt.filters)
			require.Error(t, err)
			for _, sub := range tt.errSubstr {
				require.Contains(t, err.Error(), sub)
			}
		})
	}
}
