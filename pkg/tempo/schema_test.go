package tempo

import (
	"testing"
	"time"

	apidata "github.com/grafana/grafana-plugin-sdk-go/experimental/apis/datasource/v0alpha1"
	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"
	schemas "github.com/grafana/schemads"
	"github.com/stretchr/testify/require"
)

func TestGlobalColumnValuesErrors(t *testing.T) {
	errs := globalColumnValuesErrors([]string{tempoSpanColTraceIDHidden, "resource.service.name"}, "upstream failed")
	require.Len(t, errs, 1)
	require.Equal(t, "upstream failed", errs["resource.service.name"])

	errsOnlyFixed := globalColumnValuesErrors([]string{tempoSpanColSpanID}, "no ds")
	require.Len(t, errsOnlyFixed, 1)
	require.Equal(t, "no ds", errsOnlyFixed[""])
}

func TestSpansTable_MetricsHints(t *testing.T) {
	hints := spansTableHints()
	names := make([]string, len(hints))
	for i, h := range hints {
		names[i] = h.Name
	}
	require.Contains(t, names, "rate")
	require.Contains(t, names, "step")
	require.Contains(t, names, "instant")
	require.Contains(t, names, "exemplars")
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

	require.Equal(t, schemas.ColumnTypeDatetime, byName[tempoSpanColTimestamp].Type)
	require.Equal(t, "Sample time for TraceQL metrics SQL (flattened from series). Not used for span search.", byName[tempoSpanColTimestamp].Metadata.Description)
	require.Nil(t, byName[tempoSpanColTimestamp].Operators)
	require.Equal(t, schemas.ColumnTypeFloat64, byName[tempoSpanColValue].Type)
	require.Equal(t, "Metric sample value for TraceQL metrics SQL (rate, count_over_time, etc.).", byName[tempoSpanColValue].Metadata.Description)
	require.Nil(t, byName[tempoSpanColValue].Operators)
}

func TestUnsupportedSchemadsTableError(t *testing.T) {
	require.Equal(t, `unsupported table "traces" (only "spans" is supported)`, unsupportedSchemadsTableError("traces"))
}

func TestGlobalColumnValuesErrors_UnsupportedTable(t *testing.T) {
	msg := unsupportedSchemadsTableError("traces")
	errs := globalColumnValuesErrors([]string{tempoSpanColTraceIDHidden, "name"}, msg)
	require.Len(t, errs, 1)
	require.Equal(t, msg, errs["name"])
}

func TestSpansTableColumns_IncludesMetricsAndDropsDynamicCollisions(t *testing.T) {
	got := spansTableColumns([]schemas.Column{
		{Name: "value", Metadata: schemas.Metadata{Description: "tag must lose to metrics column"}},
		{Name: "name", Metadata: schemas.Metadata{Description: "dup from tags"}},
		{Name: "duration"},
		{Name: "resource.svc"},
	})
	names := make([]string, len(got))
	for i, c := range got {
		names[i] = c.Name
	}
	require.Contains(t, names, "timestamp")
	require.Contains(t, names, "value")
	require.Contains(t, names, "name")
	require.Contains(t, names, "duration")
	require.Contains(t, names, "resource.svc")
	var valueCols, nameCols int
	for _, c := range got {
		switch c.Name {
		case "value":
			valueCols++
			require.NotEqual(t, "tag must lose to metrics column", c.Metadata.Description)
		case "name":
			nameCols++
			require.NotEqual(t, "dup from tags", c.Metadata.Description)
		}
	}
	require.Equal(t, 1, valueCols)
	require.Equal(t, 1, nameCols)
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
