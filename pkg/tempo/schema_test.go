package tempo

import (
	"testing"
	"time"

	apidata "github.com/grafana/grafana-plugin-sdk-go/experimental/apis/datasource/v0alpha1"
	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"
	schemas "github.com/grafana/schemads"
	"github.com/stretchr/testify/require"
)

func columnNames(cols []schemas.Column) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

func TestGlobalColumnValuesErrors(t *testing.T) {
	noTag := tableFixedColumnNamesNoTagValues(spansFixedColumns())
	errs := globalColumnValuesErrors([]string{tempoSpanColTraceIDHidden, "resource.service.name"}, noTag, "upstream failed")
	require.Len(t, errs, 1)
	require.Equal(t, "upstream failed", errs["resource.service.name"])

	errsOnlyFixed := globalColumnValuesErrors([]string{tempoSpanColSpanID}, noTag, "no ds")
	require.Len(t, errsOnlyFixed, 1)
	require.Equal(t, "no ds", errsOnlyFixed[""])
}

func TestTempoMetricsCapabilities_AggregateFunctions(t *testing.T) {
	require.Equal(t, []schemas.AggregateFunction{
		schemas.AggregateCount,
		schemas.AggregateAvg,
		schemas.AggregateSum,
		schemas.AggregateMin,
		schemas.AggregateMax,
	}, tempoMetricsCapabilities.AggregateFunctions)
	require.True(t, tempoMetricsCapabilities.Limit)
}

func TestSpansTableMetadata_ConciseCustom(t *testing.T) {
	meta := spansTableMetadata()
	require.Contains(t, meta.Description, "span_metrics")
	require.Contains(t, meta.Description, "GROUP BY")
	require.Contains(t, meta.Description, "not pushed to Tempo")

	patterns, ok := meta.Custom["queryPatterns"].([]map[string]string)
	require.True(t, ok)
	require.Len(t, patterns, 2)
	modes := make([]string, len(patterns))
	for i, p := range patterns {
		modes[i] = p["mode"]
		require.NotEmpty(t, p["sql"])
		require.NotContains(t, p["sql"], "\n")
	}
	require.Contains(t, modes, "span_tabular")
	require.Contains(t, modes, "span_duration")
}

func TestSpanMetricsTableMetadata_ConciseCustom(t *testing.T) {
	meta := spanMetricsTableMetadata()
	require.Contains(t, meta.Description, "FOR")
	require.Equal(t, "3h", meta.Custom["maxTimeRange"])

	patterns, ok := meta.Custom["queryPatterns"].([]map[string]string)
	require.True(t, ok)
	require.Len(t, patterns, 3)
	modes := make([]string, len(patterns))
	for i, p := range patterns {
		modes[i] = p["mode"]
		require.Contains(t, p["sql"], "span_metrics")
	}
	require.Contains(t, modes, "metrics_count")
	require.Contains(t, modes, "metrics_rate")
	require.Contains(t, modes, "metrics_duration")
}

func TestSpansTable_IncludesMetadata(t *testing.T) {
	table := spansTable(nil)
	require.Equal(t, tempoSchemadsTableSpans, table.Name)
	require.NotEmpty(t, table.Metadata.Description)
	require.NotNil(t, table.Metadata.Custom["queryPatterns"])
	require.Empty(t, table.TableHints)
}

func TestSpansTable_ExcludesMetricsColumns(t *testing.T) {
	names := columnNames(spansTableColumns(nil))
	require.NotContains(t, names, "timestamp")
	require.NotContains(t, names, "value")
}

func TestSpanMetricsTable_HasMetricsShape(t *testing.T) {
	table := spanMetricsTable(nil)
	require.Equal(t, tempoSchemadsTableSpanMetrics, table.Name)
	require.NotEmpty(t, table.TableHints)
	require.Equal(t, tempoMetricsCapabilities, table.Capabilities)
	names := columnNames(table.Columns)
	require.Contains(t, names, "timestamp")
	require.Contains(t, names, "value")
	require.Contains(t, names, "traceIdHidden")
	require.Contains(t, names, "spanID")
	require.NotContains(t, names, "time")
}

func TestSpansTable_HasNoCapabilities(t *testing.T) {
	table := spansTable(nil)
	require.Nil(t, table.Capabilities)
}

func TestSchema_ReturnsBothTables(t *testing.T) {
	require.Equal(t, []string{tempoSchemadsTableSpans, tempoSchemadsTableSpanMetrics}, tempoSchemadsTables())
	require.Len(t, tempoSchemadsTables(), 2)
}

func TestSpanMetricsTable_MetricsHints(t *testing.T) {
	hints := spanMetricsTableHints()
	names := make([]string, len(hints))
	for i, h := range hints {
		names[i] = h.Name
	}
	require.Contains(t, names, "rate")
	require.Contains(t, names, "step")
	require.Contains(t, names, "instant")
	require.Contains(t, names, "exemplars")

	var rateHint *schemas.TableHint
	for i := range hints {
		if hints[i].Name == "rate" {
			rateHint = &hints[i]
			break
		}
	}
	require.NotNil(t, rateHint)
	require.Contains(t, rateHint.Description, "FOR (rate())")
	require.Contains(t, rateHint.Description, "Unlike Loki")
}

func TestSpansFixedColumnsSupportsValues(t *testing.T) {
	for _, c := range spansFixedColumns() {
		require.NotNil(t, c.SupportsValues, "column %q", c.Name)
		switch c.Name {
		case tempoSpanColName, tempoSpanColDuration:
			require.True(t, *c.SupportsValues, "column %q", c.Name)
		default:
			require.False(t, *c.SupportsValues, "column %q", c.Name)
		}
	}
}

func TestSpansFixedColumnsOperators(t *testing.T) {
	cols := spansFixedColumns()
	byName := make(map[string]schemas.Column, len(cols))
	for _, c := range cols {
		byName[c.Name] = c
	}

	require.Empty(t, byName[tempoSpanColTime].Operators, "time is display-only; TraceQL has no span start-time intrinsic")

	require.Equal(t, traceqlIdentifierColumnOperators(), byName[tempoSpanColTraceIDHidden].Operators)
	require.Equal(t, traceqlIdentifierColumnOperators(), byName[tempoSpanColSpanID].Operators)
	require.Equal(t, traceqlStringColumnOperators(), byName[tempoSpanColName].Operators)
	require.Equal(t, traceqlDurationColumnOperators(), byName[tempoSpanColDuration].Operators)
}

func TestSpanMetricsFixedColumnsOperators(t *testing.T) {
	cols := spanMetricsFixedColumns()
	byName := make(map[string]schemas.Column, len(cols))
	for _, c := range cols {
		byName[c.Name] = c
	}

	require.Equal(t, schemas.ColumnTypeDatetime, byName[tempoSpanColTimestamp].Type)
	require.Nil(t, byName[tempoSpanColTimestamp].Operators)
	require.Equal(t, schemas.ColumnTypeFloat64, byName[tempoSpanColValue].Type)
	require.Contains(t, byName[tempoSpanColValue].Metadata.Description, "output only")
	require.Nil(t, byName[tempoSpanColValue].Operators)
}

func TestUnsupportedSchemadsTableError(t *testing.T) {
	require.Equal(t, `unsupported table "traces" (supported: "spans", "span_metrics")`, unsupportedSchemadsTableError("traces"))
}

func TestGlobalColumnValuesErrors_UnsupportedTable(t *testing.T) {
	msg := unsupportedSchemadsTableError("traces")
	noTag := tableFixedColumnNamesNoTagValues(spansFixedColumns())
	errs := globalColumnValuesErrors([]string{tempoSpanColTraceIDHidden, "name"}, noTag, msg)
	require.Len(t, errs, 1)
	require.Equal(t, msg, errs["name"])
}

func TestSpansTableColumns_DropsDynamicCollisions(t *testing.T) {
	got := spansTableColumns([]schemas.Column{
		{Name: "name", Metadata: schemas.Metadata{Description: "dup from tags"}},
		{Name: "duration"},
		{Name: "resource.svc"},
	})
	names := columnNames(got)
	require.Contains(t, names, "name")
	require.Contains(t, names, "duration")
	require.Contains(t, names, "resource.svc")
	require.NotContains(t, names, "value")
	require.NotContains(t, names, "timestamp")
	var nameCols int
	for _, c := range got {
		if c.Name == "name" {
			nameCols++
			require.NotEqual(t, "dup from tags", c.Metadata.Description)
		}
	}
	require.Equal(t, 1, nameCols)
}

func TestSpanMetricsTableColumns_DropsDynamicValueCollision(t *testing.T) {
	got := spanMetricsTableColumns([]schemas.Column{
		{Name: "value", Metadata: schemas.Metadata{Description: "tag must lose to metrics column"}},
	})
	var valueCols int
	for _, c := range got {
		if c.Name == "value" {
			valueCols++
			require.NotEqual(t, "tag must lose to metrics column", c.Metadata.Description)
		}
	}
	require.Equal(t, 1, valueCols)
}

func TestFlattenTempoSearchTagScopesToColumnNames(t *testing.T) {
	scopes := []tempoSearchTagScope{
		{Name: string(dataquery.TraceqlSearchScopeIntrinsic), Tags: []string{"name", "status"}},
		{Name: string(dataquery.TraceqlSearchScopeResource), Tags: []string{"service.name", "cluster"}},
		{Name: string(dataquery.TraceqlSearchScopeSpan), Tags: []string{"db"}},
	}
	got := flattenTempoSearchTagScopesToColumnNames(scopes)
	require.Equal(t, []string{
		"name",
		"resource.cluster",
		"resource.service.name",
		"span.db",
		"status",
	}, got)
}

func TestTagColumnNamesSetFromScopes(t *testing.T) {
	scopes := []tempoSearchTagScope{
		{Name: string(dataquery.TraceqlSearchScopeIntrinsic), Tags: []string{"status"}},
		{Name: string(dataquery.TraceqlSearchScopeResource), Tags: []string{"service.name"}},
	}
	set := tagColumnNamesSetFromScopes(scopes)
	_, hasStatus := set["status"]
	_, hasSvc := set["resource.service.name"]
	require.True(t, hasStatus)
	require.True(t, hasSvc)
	_, hasUnknown := set["not.a.tag"]
	require.False(t, hasUnknown)
}

func TestFlattenTempoSearchTagScopesToColumnNames_Dedupes(t *testing.T) {
	scopes := []tempoSearchTagScope{
		{Name: string(dataquery.TraceqlSearchScopeIntrinsic), Tags: []string{"name"}},
		{Name: string(dataquery.TraceqlSearchScopeIntrinsic), Tags: []string{"name", "status"}},
	}
	got := flattenTempoSearchTagScopesToColumnNames(scopes)
	require.Equal(t, []string{"name", "status"}, got)
}

func TestParseFlexibleTimeForTagValues(t *testing.T) {
	ts, err := parseFlexibleTimeForTagValues("2024-01-02T15:04:05Z")
	require.NoError(t, err)
	require.Equal(t, time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC), ts.UTC())

	ms, err := parseFlexibleTimeForTagValues("1704205445000")
	require.NoError(t, err)
	require.Equal(t, int64(1704205445000), ms.UnixMilli())
}

func TestTimeRangeToUnixForTempoTagAPI(t *testing.T) {
	start, end := timeRangeToUnixForTempoTagAPI(apidata.TimeRange{
		From: "2024-01-01T00:00:00Z",
		To:   "2024-01-02T00:00:00Z",
	})
	require.Equal(t, int64(1704067200), start)
	require.Equal(t, int64(1704153600), end)

	start2, end2 := timeRangeToUnixForTempoTagAPI(apidata.TimeRange{})
	require.Greater(t, end2, start2)
	require.GreaterOrEqual(t, end2-start2, int64(tempoDefaultTagValuesLookbackSec)-200)
}
