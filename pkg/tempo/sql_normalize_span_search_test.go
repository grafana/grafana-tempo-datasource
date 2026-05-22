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

func TestNormalizeGrafanaSQLRequest_DisabledToggle(t *testing.T) {
	ds := &DataSource{}
	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: config.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: "",
			}),
		},
		Queries: []backend.DataQuery{{
			RefID: "A",
			JSON:  []byte(`{"grafanaSql":true,"table":"spans"}`),
		}},
	}
	out, errs, metricsRefIDs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Nil(t, metricsRefIDs)
	require.Equal(t, req, out)
}

func TestNormalizeGrafanaSQLRequest_NotGrafanaSqlPassthrough(t *testing.T) {
	ds := &DataSource{}
	orig := []byte(`{"refId":"A","queryType":"traceqlSearch","query":"{}"}`)
	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: config.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: "dsAbstractionApp",
			}),
		},
		Queries: []backend.DataQuery{{
			RefID:     "A",
			QueryType: string(dataquery.TempoQueryTypeTraceqlSearch),
			JSON:      orig,
		}},
	}
	out, errs, metricsRefIDs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Nil(t, errs)
	require.Nil(t, metricsRefIDs)
	require.Len(t, out.Queries, 1)
	require.JSONEq(t, string(orig), string(out.Queries[0].JSON))
}

func TestNormalizeGrafanaSQLRequest_ConvertsSpansQuery(t *testing.T) {
	ds := &DataSource{}
	sq := schemas.Query{
		Table:      tempoSchemadsTableSpans,
		GrafanaSql: true,
		Filters: []schemas.ColumnFilter{
			{
				Name: "resource.service.name",
				Conditions: []schemas.FilterCondition{{
					Operator: schemas.OperatorEquals,
					Value:    "api",
				}},
			},
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
	require.Nil(t, metricsRefIDs)
	require.Len(t, out.Queries, 1)
	require.Equal(t, string(dataquery.TempoQueryTypeTraceql), out.Queries[0].QueryType)

	var model dataquery.TempoQuery
	require.NoError(t, json.Unmarshal(out.Queries[0].JSON, &model))
	require.NotNil(t, model.QueryType)
	require.Equal(t, string(dataquery.TempoQueryTypeTraceql), *model.QueryType)
	require.NotNil(t, model.Query)
	require.Equal(t, `{resource.service.name="api"}`, *model.Query)
	require.NotNil(t, model.TableType)
	require.Equal(t, dataquery.SearchTableTypeSpans, *model.TableType)
}

func TestNormalizeGrafanaSQLRequest_UnsupportedTable(t *testing.T) {
	ds := &DataSource{}
	sq := schemas.Query{Table: "other", GrafanaSql: true}
	raw, err := json.Marshal(sq)
	require.NoError(t, err)
	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: config.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: "dsAbstractionApp",
			}),
		},
		Queries: []backend.DataQuery{{RefID: "X", JSON: raw}},
	}
	out, errs, metricsRefIDs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Contains(t, errs["X"].Error(), "unsupported table")
	require.Nil(t, metricsRefIDs)
	require.Empty(t, out.Queries)
}

func TestNormalizeGrafanaSQLRequest_GrafanaSqlMissingTable(t *testing.T) {
	ds := &DataSource{}
	sq := schemas.Query{GrafanaSql: true, Table: "   "}
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
	require.Contains(t, errs["A"].Error(), "table is required")
	require.Nil(t, metricsRefIDs)
	require.Empty(t, out.Queries)
}

func TestNormalizeGrafanaSQLRequest_GrafanaSqlOmittedTable(t *testing.T) {
	ds := &DataSource{}
	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: config.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: "dsAbstractionApp",
			}),
		},
		Queries: []backend.DataQuery{{RefID: "B", JSON: []byte(`{"grafanaSql":true}`)}},
	}
	out, errs, metricsRefIDs := ds.normalizeGrafanaSQLRequest(context.Background(), req)
	require.Contains(t, errs["B"].Error(), "table is required")
	require.Nil(t, metricsRefIDs)
	require.Empty(t, out.Queries)
}
