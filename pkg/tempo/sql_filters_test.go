package tempo

import (
	"testing"

	"github.com/stretchr/testify/require"

	schemas "github.com/grafana/schemads"
)

func TestTraceQLFromSchemadsFilters_Empty(t *testing.T) {
	q, err := traceQLFromSchemadsFilters(nil)
	require.NoError(t, err)
	require.Equal(t, "{}", q)
}

func TestTraceQLFromSchemadsFilters_TimeColumnRejected(t *testing.T) {
	_, err := traceQLFromSchemadsFilters([]schemas.ColumnFilter{{
		Name: tempoSpanColTime,
		Conditions: []schemas.FilterCondition{{
			Operator: schemas.OperatorGreaterThan,
			Value:    "2024-01-01",
		}},
	}})
	require.Error(t, err)
}

func TestTraceQLFromSchemadsFilters_SpanScopeAndIntrinsic(t *testing.T) {
	q, err := traceQLFromSchemadsFilters([]schemas.ColumnFilter{
		{
			Name: "span.db",
			Conditions: []schemas.FilterCondition{{
				Operator: schemas.OperatorEquals,
				Value:    "postgres",
			}},
		},
		{
			Name: "status",
			Conditions: []schemas.FilterCondition{{
				Operator: schemas.OperatorEquals,
				Value:    float64(200),
			}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, `{span.db="postgres" && status=200}`, q)
}

func TestTraceQLFromSchemadsFilters_LikeAnchorsFullString(t *testing.T) {
	q, err := traceQLFromSchemadsFilters([]schemas.ColumnFilter{{
		Name: "name",
		Conditions: []schemas.FilterCondition{{
			Operator: schemas.OperatorLike,
			Value:    "foo",
		}},
	}})
	require.NoError(t, err)
	require.Equal(t, `{name=~"^foo$"}`, q)
}

func TestTraceQLFromSchemadsFilters_LikeWildcard(t *testing.T) {
	q, err := traceQLFromSchemadsFilters([]schemas.ColumnFilter{{
		Name: "name",
		Conditions: []schemas.FilterCondition{{
			Operator: schemas.OperatorLike,
			Value:    "%foo%",
		}},
	}})
	require.NoError(t, err)
	require.Equal(t, `{name=~"^.*foo.*$"}`, q)
}

func TestTraceQLFromSchemadsFilters_JoinPipeUnsupportedValueType(t *testing.T) {
	_, err := traceQLFromSchemadsFilters([]schemas.ColumnFilter{{
		Name: "name",
		Conditions: []schemas.FilterCondition{{
			Operator: schemas.OperatorEquals,
			Values:   []any{"a", map[string]any{"x": 1}},
		}},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "converting multi-value match operand")
	require.Contains(t, err.Error(), "unsupported value type")
}

func TestTraceQLFromSchemadsFilters_MultiValueNullOperand(t *testing.T) {
	_, err := traceQLFromSchemadsFilters([]schemas.ColumnFilter{{
		Name: "name",
		Conditions: []schemas.FilterCondition{{
			Operator: schemas.OperatorEquals,
			Values:   []any{"a", nil},
		}},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "converting multi-value match operand")
	require.Contains(t, err.Error(), "value is null")
}
