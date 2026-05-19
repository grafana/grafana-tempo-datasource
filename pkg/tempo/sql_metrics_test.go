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

	out, errs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Len(t, out.Queries, 1)
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

	out, errs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Len(t, out.Queries, 1)

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

	out, errs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Len(t, out.Queries, 1)

	var model dataquery.TempoQuery
	require.NoError(t, json.Unmarshal(out.Queries[0].JSON, &model))
	require.NotNil(t, model.Query)
	require.Equal(t, `{resource.service.name="api"}`, *model.Query)
	require.NotContains(t, *model.Query, "|")
	require.NotNil(t, model.Limit)
	require.Equal(t, int64(50), *model.Limit)
}

func int64Ptr(n int64) *int64 {
	return &n
}
