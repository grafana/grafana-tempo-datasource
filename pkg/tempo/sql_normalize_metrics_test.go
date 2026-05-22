package tempo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/config"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/featuretoggles"
	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"
	"github.com/stretchr/testify/require"

	schemas "github.com/grafana/schemads"
)

func TestBuildTraceQLMetricsQuery_CountWithGroupBy(t *testing.T) {
	sq := schemadsQuery{
		Query: schemas.Query{
			Filters: []schemas.ColumnFilter{{
				Name: "resource.service.name",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorEquals,
					Value:    "api",
				}},
			}},
		},
		Aggregation: &aggregationHint{
			Function: "COUNT",
			GroupBy:  []string{"resource.service.name"},
		},
	}
	q, err := buildTraceQLMetricsQuery(sq)
	require.NoError(t, err)
	require.Equal(t, `{resource.service.name="api"} | count_over_time() by (resource.service.name)`, q)
}

func TestBuildTraceQLMetricsQuery_AvgOverDuration(t *testing.T) {
	sq := schemadsQuery{
		Query: schemas.Query{},
		Aggregation: &aggregationHint{
			Function: "AVG",
			Column:   "duration",
		},
	}
	q, err := buildTraceQLMetricsQuery(sq)
	require.NoError(t, err)
	require.Equal(t, `{} | avg_over_time(span:duration)`, q)
}

func TestBuildTraceQLMetricsQuery_RateHint(t *testing.T) {
	sq := schemadsQuery{
		Query: schemas.Query{
			TableHintValues: map[string]string{"RATE": ""},
		},
		Aggregation: &aggregationHint{
			Function: "COUNT",
		},
	}
	q, err := buildTraceQLMetricsQuery(sq)
	require.NoError(t, err)
	require.Equal(t, `{} | rate()`, q)
}

func TestNormalizeGrafanaSQLRequest_MetricsQuery(t *testing.T) {
	ds := &DataSource{}
	sq := schemadsQuery{
		Query: schemas.Query{
			Table:      tempoSchemadsTableSpans,
			GrafanaSql: true,
			Filters: []schemas.ColumnFilter{{
				Name: "resource.service.name",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorEquals,
					Value:    "api",
				}},
			}},
			Limit: int64Ptr(100),
		},
		Aggregation: &aggregationHint{
			Function: "COUNT",
			GroupBy:  []string{"resource.service.name"},
		},
	}
	raw, err := json.Marshal(sq)
	require.NoError(t, err)

	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: config.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: "dsAbstractionApp",
			}),
		},
		Queries: []backend.DataQuery{{RefID: "A", JSON: raw}},
	}

	out, errs, metricsRefIDs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Len(t, out.Queries, 1)
	require.Contains(t, metricsRefIDs, "A")
	require.Equal(t, string(dataquery.TempoQueryTypeTraceql), out.Queries[0].QueryType)

	var model dataquery.TempoQuery
	require.NoError(t, json.Unmarshal(out.Queries[0].JSON, &model))
	require.NotNil(t, model.QueryType)
	require.Equal(t, string(dataquery.TempoQueryTypeTraceql), *model.QueryType)
	require.NotNil(t, model.Query)
	require.Equal(t, `{resource.service.name="api"} | count_over_time() by (resource.service.name)`, *model.Query)
	require.NotNil(t, model.TableType)
	require.Equal(t, dataquery.SearchTableTypeSpans, *model.TableType)
	require.Nil(t, model.Limit)
}

func TestNormalizeGrafanaSQLRequest_MetricsStepAndInstant(t *testing.T) {
	ds := &DataSource{}
	sq := schemadsQuery{
		Query: schemas.Query{
			Table:           tempoSchemadsTableSpans,
			GrafanaSql:      true,
			TableHintValues: map[string]string{"STEP": "30s", "INSTANT": ""},
		},
		Aggregation: &aggregationHint{Function: "COUNT"},
	}
	raw, err := json.Marshal(sq)
	require.NoError(t, err)

	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: config.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: "dsAbstractionApp",
			}),
		},
		Queries: []backend.DataQuery{{RefID: "A", JSON: raw}},
	}

	out, errs, metricsRefIDs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Len(t, out.Queries, 1)
	require.Contains(t, metricsRefIDs, "A")

	var model dataquery.TempoQuery
	require.NoError(t, json.Unmarshal(out.Queries[0].JSON, &model))
	require.NotNil(t, model.Step)
	require.Equal(t, "30s", *model.Step)
	require.NotNil(t, model.MetricsQueryType)
	require.Equal(t, dataquery.MetricsQueryTypeInstant, *model.MetricsQueryType)
}

func TestNormalizeGrafanaSQLRequest_MetricsStillSpanSearchWithoutAggregation(t *testing.T) {
	ds := &DataSource{}
	sq := schemas.Query{
		Table:      tempoSchemadsTableSpans,
		GrafanaSql: true,
		Filters: []schemas.ColumnFilter{{
			Name: "resource.service.name",
			Conditions: []schemas.FilterCondition{{
				Operator: schemas.OperatorEquals,
				Value:    "api",
			}},
		}},
		Limit: int64Ptr(50),
	}
	raw, err := json.Marshal(sq)
	require.NoError(t, err)

	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: config.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: "dsAbstractionApp",
			}),
		},
		Queries: []backend.DataQuery{{RefID: "A", JSON: raw}},
	}

	out, errs, metricsRefIDs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Nil(t, metricsRefIDs)
	require.Len(t, out.Queries, 1)

	var model dataquery.TempoQuery
	require.NoError(t, json.Unmarshal(out.Queries[0].JSON, &model))
	require.NotNil(t, model.Query)
	require.Equal(t, `{resource.service.name="api"}`, *model.Query)
	require.NotContains(t, *model.Query, "|")
	require.NotNil(t, model.Limit)
	require.Equal(t, int64(50), *model.Limit)
}

// Multi-query panels can mix span-search SQL, metrics SQL, and non-SQL queries in one request.
// metricsRefIDs must list only refIDs that need response flattening.
func TestNormalizeGrafanaSQLRequest_MixedSpanSearchAndMetrics(t *testing.T) {
	ds := &DataSource{}
	pluginCtx := backend.PluginContext{
		GrafanaConfig: config.NewGrafanaCfg(map[string]string{
			featuretoggles.EnabledFeatures: "dsAbstractionApp",
		}),
	}

	spanSearchRaw, err := json.Marshal(schemas.Query{
		Table:      tempoSchemadsTableSpans,
		GrafanaSql: true,
		Filters: []schemas.ColumnFilter{{
			Name: "name",
			Conditions: []schemas.FilterCondition{{
				Operator: schemas.OperatorEquals,
				Value:    "GET",
			}},
		}},
	})
	require.NoError(t, err)

	metricsRaw, err := json.Marshal(schemadsQuery{
		Query: schemas.Query{
			Table:      tempoSchemadsTableSpans,
			GrafanaSql: true,
		},
		Aggregation: &aggregationHint{Function: "COUNT"},
	})
	require.NoError(t, err)

	legacyPassthrough := []byte(`{"refId":"C","queryType":"traceqlSearch","query":"{}"}`)

	req := &backend.QueryDataRequest{
		PluginContext: pluginCtx,
		Queries: []backend.DataQuery{
			{RefID: "A", JSON: spanSearchRaw},
			{RefID: "B", JSON: metricsRaw},
			{RefID: "C", QueryType: string(dataquery.TempoQueryTypeTraceqlSearch), JSON: legacyPassthrough},
		},
	}

	out, errs, metricsRefIDs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Len(t, out.Queries, 3)
	require.Len(t, metricsRefIDs, 1)
	require.Contains(t, metricsRefIDs, "B")
	require.NotContains(t, metricsRefIDs, "A")
	require.NotContains(t, metricsRefIDs, "C")

	byRef := make(map[string]backend.DataQuery, len(out.Queries))
	for _, q := range out.Queries {
		byRef[q.RefID] = q
	}

	var spanModel dataquery.TempoQuery
	require.NoError(t, json.Unmarshal(byRef["A"].JSON, &spanModel))
	require.NotNil(t, spanModel.Query)
	require.Equal(t, `{name="GET"}`, *spanModel.Query)
	require.NotContains(t, *spanModel.Query, "|")

	var metricsModel dataquery.TempoQuery
	require.NoError(t, json.Unmarshal(byRef["B"].JSON, &metricsModel))
	require.NotNil(t, metricsModel.Query)
	require.Equal(t, `{} | count_over_time()`, *metricsModel.Query)

	require.JSONEq(t, string(legacyPassthrough), string(byRef["C"].JSON))
}

func int64Ptr(n int64) *int64 {
	return &n
}
