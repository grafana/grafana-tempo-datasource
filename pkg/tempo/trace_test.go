package tempo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// traceIDModel builds a *dataquery.TempoQuery whose Query (trace ID) field
// points at a copy of id, matching the &dataquery.TempoQuery{Query: &x}
// pattern used throughout this package's tests.
func traceIDModel(id string) *dataquery.TempoQuery {
	return &dataquery.TempoQuery{Query: &id}
}

// newV2Params lists every Tempo HTTP query param introduced for the trace-id-tab
// span pruning / filter options, all gated on TraceRequestApiVersionV2.
var newV2Params = []string{
	"span_pruning",
	"span_pruning_group_by",
	"span_pruning_min_spans",
	"span_pruning_max_parent_depth",
	"q",
	"keep_hierarchy",
	"match_depth",
	"ancestor_depth",
}

func TestCreateRequestNewV2Params(t *testing.T) {
	boolTrue := true
	groupBy := "service.name"
	var minSpans int64 = 5
	var maxParentDepth int64 = 3
	filterQuery := "{status=error}"
	var matchDepth int64 = 2

	tests := []struct {
		name      string
		model     *dataquery.TempoQuery
		wantParam string
		wantValue string
	}{
		{
			name:      "SpanPruning",
			model:     &dataquery.TempoQuery{SpanPruning: &boolTrue},
			wantParam: "span_pruning",
			wantValue: "true",
		},
		{
			name:      "SpanPruningGroupBy",
			model:     &dataquery.TempoQuery{SpanPruningGroupBy: &groupBy},
			wantParam: "span_pruning_group_by",
			wantValue: groupBy,
		},
		{
			name:      "SpanPruningMinSpans",
			model:     &dataquery.TempoQuery{SpanPruningMinSpans: &minSpans},
			wantParam: "span_pruning_min_spans",
			wantValue: "5",
		},
		{
			name:      "SpanPruningMaxParentDepth",
			model:     &dataquery.TempoQuery{SpanPruningMaxParentDepth: &maxParentDepth},
			wantParam: "span_pruning_max_parent_depth",
			wantValue: "3",
		},
		{
			name:      "FilterQuery",
			model:     &dataquery.TempoQuery{FilterQuery: &filterQuery},
			wantParam: "q",
			wantValue: filterQuery,
		},
		{
			name:      "KeepHierarchy",
			model:     &dataquery.TempoQuery{KeepHierarchy: &boolTrue},
			wantParam: "keep_hierarchy",
			wantValue: "true",
		},
		{
			name:      "MatchDepth",
			model:     &dataquery.TempoQuery{MatchDepth: &matchDepth},
			wantParam: "match_depth",
			wantValue: "2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			traceID := "abc123"
			tc.model.Query = &traceID
			service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
			req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, tc.model, 0, 0)
			require.NoError(t, err)
			require.Equal(t, tc.wantValue, req.URL.Query().Get(tc.wantParam))
		})
	}
}

func TestTempo(t *testing.T) {
	t.Run("createRequest v1 without time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV1, traceIDModel("abc123"), 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/traces/abc123", req.URL.String())
	})

	t.Run("createRequest v1 with time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV1, traceIDModel("abc123"), 1, 2)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/traces/abc123?end=2&start=1", req.URL.String())
	})

	t.Run("createRequest v2 without time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, traceIDModel("abc123"), 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/v2/traces/abc123", req.URL.String())
	})

	t.Run("createRequest v2 with time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, traceIDModel("abc123"), 1, 2)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/v2/traces/abc123?end=2&start=1", req.URL.String())
	})

	t.Run("createRequest v1 with trailing slash URL - no double slash", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/"}, TraceRequestApiVersionV1, traceIDModel("abc123"), 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/traces/abc123", req.URL.String())
	})

	t.Run("createRequest v2 with trailing slash URL - no double slash", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/"}, TraceRequestApiVersionV2, traceIDModel("abc123"), 1, 2)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/v2/traces/abc123?end=2&start=1", req.URL.String())
	})

	t.Run("createRequest v2 without trailing slash URL - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, traceIDModel("abc123"), 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/v2/traces/abc123", req.URL.String())
	})

	t.Run("createRequest preserves existing query params in the configured URL", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/routing?my_arg=1"}, TraceRequestApiVersionV2, traceIDModel("abc123"), 1, 2)
		require.NoError(t, err)
		// The custom my_arg must survive and start/end are appended, not concatenated with a second "?".
		assert.Equal(t, "http://tempo/routing/api/v2/traces/abc123?end=2&my_arg=1&start=1", req.URL.String())
	})

	t.Run("createRequest preserves existing query params without a time range", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/routing?my_arg=1"}, TraceRequestApiVersionV2, traceIDModel("abc123"), 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/routing/api/v2/traces/abc123?my_arg=1", req.URL.String())
	})

	t.Run("createRequest v2 with no new fields set omits all new params", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, traceIDModel("abc123"), 0, 0)
		require.NoError(t, err)
		for _, param := range newV2Params {
			_, present := req.URL.Query()[param]
			require.False(t, present, "expected %s to be absent when unset", param)
		}
	})

	t.Run("createRequest v2 AncestorDepth omitted when KeepHierarchy is nil", func(t *testing.T) {
		traceID := "abc123"
		var ancestorDepth int64 = 4
		model := &dataquery.TempoQuery{Query: &traceID, AncestorDepth: &ancestorDepth}
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, model, 0, 0)
		require.NoError(t, err)
		_, present := req.URL.Query()["ancestor_depth"]
		require.False(t, present, "ancestor_depth must not be sent without keep_hierarchy=true")
	})

	t.Run("createRequest v2 AncestorDepth omitted when KeepHierarchy is false", func(t *testing.T) {
		traceID := "abc123"
		var ancestorDepth int64 = 4
		keepHierarchy := false
		model := &dataquery.TempoQuery{Query: &traceID, AncestorDepth: &ancestorDepth, KeepHierarchy: &keepHierarchy}
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, model, 0, 0)
		require.NoError(t, err)
		_, present := req.URL.Query()["ancestor_depth"]
		require.False(t, present, "ancestor_depth must not be sent when keep_hierarchy=false")
	})

	t.Run("createRequest v2 AncestorDepth included when KeepHierarchy is true", func(t *testing.T) {
		traceID := "abc123"
		var ancestorDepth int64 = 4
		keepHierarchy := true
		model := &dataquery.TempoQuery{Query: &traceID, AncestorDepth: &ancestorDepth, KeepHierarchy: &keepHierarchy}
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, model, 0, 0)
		require.NoError(t, err)
		require.Equal(t, "4", req.URL.Query().Get("ancestor_depth"))
	})

	t.Run("createRequest v1 never includes new v2 params even when all are set", func(t *testing.T) {
		traceID := "abc123"
		boolTrue := true
		groupBy := "service.name"
		var minSpans int64 = 5
		var maxParentDepth int64 = 3
		filterQuery := "{status=error}"
		var matchDepth int64 = 2
		var ancestorDepth int64 = 4
		model := &dataquery.TempoQuery{
			Query:                     &traceID,
			SpanPruning:               &boolTrue,
			SpanPruningGroupBy:        &groupBy,
			SpanPruningMinSpans:       &minSpans,
			SpanPruningMaxParentDepth: &maxParentDepth,
			FilterQuery:               &filterQuery,
			KeepHierarchy:             &boolTrue,
			MatchDepth:                &matchDepth,
			AncestorDepth:             &ancestorDepth,
		}
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV1, model, 0, 0)
		require.NoError(t, err)
		require.Equal(t, "http://tempo/api/traces/abc123", req.URL.String())
	})

	t.Run("getTrace v1 empty ResourceSpans returns downstream error", func(t *testing.T) {
		v1Called := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/api/v2/traces/") {
				w.WriteHeader(http.StatusNotFound) // trigger v1 fallback
			} else if strings.Contains(r.URL.Path, "/api/traces/") {
				v1Called = true
				w.WriteHeader(http.StatusOK) // empty body → empty ResourceSpans → nil frame
			}
		}))
		defer server.Close()

		service := &DataSource{
			info:   &DatasourceInfo{URL: server.URL, HTTPClient: server.Client()},
			logger: backend.NewLoggerWith("logger", "tempo-test"),
		}

		pluginCtx := backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{URL: server.URL},
		}
		from := time.Unix(1000, 0).UTC()
		to := time.Unix(2000, 0).UTC()
		query := backend.DataQuery{
			JSON:      []byte(`{"query": "abc123"}`),
			TimeRange: backend.TimeRange{From: from, To: to},
		}

		res, err := service.getTrace(context.Background(), pluginCtx, query)

		assert.True(t, v1Called, "expected v1 endpoint (/api/traces/) to be called")
		assert.Nil(t, res)
		require.Error(t, err)
		assert.True(t, backend.IsDownstreamError(err))
		// When no trace is found the error should mention the searched time range
		// and hint that the trace may exist outside of it (issue #176).
		assert.Contains(t, err.Error(), "abc123")
		assert.Contains(t, err.Error(), from.Format(time.RFC3339))
		assert.Contains(t, err.Error(), to.Format(time.RFC3339))
		assert.Contains(t, err.Error(), "outside")
	})

	t.Run("getTrace with zero time range omits the range from the not-found error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/api/v2/traces/") {
				w.WriteHeader(http.StatusNotFound) // trigger v1 fallback
			} else if strings.Contains(r.URL.Path, "/api/traces/") {
				w.WriteHeader(http.StatusOK) // empty body → empty ResourceSpans → nil frame
			}
		}))
		defer server.Close()

		service := &DataSource{
			info:   &DatasourceInfo{URL: server.URL, HTTPClient: server.Client()},
			logger: backend.NewLoggerWith("logger", "tempo-test"),
		}

		pluginCtx := backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{URL: server.URL},
		}
		// Zero range: with time shift off (the default) the frontend zeroes the
		// range, so no range is actually applied and Tempo searches all time.
		query := backend.DataQuery{
			JSON:      []byte(`{"query": "abc123"}`),
			TimeRange: backend.TimeRange{From: time.Unix(0, 0).UTC(), To: time.Unix(0, 0).UTC()},
		}

		res, err := service.getTrace(context.Background(), pluginCtx, query)

		assert.Nil(t, res)
		require.Error(t, err)
		assert.True(t, backend.IsDownstreamError(err))
		assert.Contains(t, err.Error(), "abc123")
		// No range was applied, so the misleading [1970-.. to 1970-..] window and
		// the "outside" hint must not appear.
		assert.NotContains(t, err.Error(), "1970")
		assert.NotContains(t, err.Error(), "outside")
	})

	t.Run("getTrace non-200 HTML response returns friendly error without raw HTML", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html><head><title>502 Bad Gateway</title></head><body>proxy error</body></html>"))
		}))
		defer server.Close()

		service := &DataSource{
			info:   &DatasourceInfo{URL: server.URL, HTTPClient: server.Client()},
			logger: backend.NewLoggerWith("logger", "tempo-test"),
		}
		pluginCtx := backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{URL: server.URL},
		}
		query := backend.DataQuery{JSON: []byte(`{"query": "abc123"}`)}

		res, err := service.getTrace(context.Background(), pluginCtx, query)

		assert.Nil(t, res)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "<html", "raw HTML must not leak into the error message")
		assert.NotContains(t, err.Error(), "<body", "raw HTML must not leak into the error message")
		assert.Contains(t, err.Error(), "unavailable", "should hint the instance may be unavailable / behind a proxy")
	})

	t.Run("getTrace non-200 JSON body is preserved", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid TraceQL"}`))
		}))
		defer server.Close()

		service := &DataSource{
			info:   &DatasourceInfo{URL: server.URL, HTTPClient: server.Client()},
			logger: backend.NewLoggerWith("logger", "tempo-test"),
		}
		pluginCtx := backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{URL: server.URL},
		}
		query := backend.DataQuery{JSON: []byte(`{"query": "abc123"}`)}

		res, err := service.getTrace(context.Background(), pluginCtx, query)

		assert.Nil(t, res)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid TraceQL", "Tempo's JSON error detail must be preserved (#203)")
	})
}
