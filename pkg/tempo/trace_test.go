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

func TestTempo(t *testing.T) {
	t.Run("createRequest v1 without time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV1, &dataquery.TempoQuery{}, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/traces/abc123", req.URL.String())
	})

	t.Run("createRequest v1 with time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV1, &dataquery.TempoQuery{}, "abc123", 1, 2)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/traces/abc123?end=2&start=1", req.URL.String())
	})

	t.Run("createRequest v2 without time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, &dataquery.TempoQuery{}, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/v2/traces/abc123?span_pruning=true", req.URL.String())
	})

	t.Run("createRequest v2 with time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, &dataquery.TempoQuery{}, "abc123", 1, 2)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/v2/traces/abc123?end=2&span_pruning=true&start=1", req.URL.String())
	})

	t.Run("createRequest v1 with trailing slash URL - no double slash", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/"}, TraceRequestApiVersionV1, &dataquery.TempoQuery{}, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/traces/abc123", req.URL.String())
	})

	t.Run("createRequest v2 with trailing slash URL - no double slash", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/"}, TraceRequestApiVersionV2, &dataquery.TempoQuery{}, "abc123", 1, 2)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/v2/traces/abc123?end=2&span_pruning=true&start=1", req.URL.String())
	})

	t.Run("createRequest v2 without trailing slash URL - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, &dataquery.TempoQuery{}, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/v2/traces/abc123?span_pruning=true", req.URL.String())
	})

	t.Run("createRequest preserves existing query params in the configured URL", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/routing?my_arg=1"}, TraceRequestApiVersionV2, &dataquery.TempoQuery{}, "abc123", 1, 2)
		require.NoError(t, err)
		// The custom my_arg must survive and start/end are appended, not concatenated with a second "?".
		assert.Equal(t, "http://tempo/routing/api/v2/traces/abc123?end=2&my_arg=1&span_pruning=true&start=1", req.URL.String())
	})

	t.Run("createRequest preserves existing query params without a time range", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/routing?my_arg=1"}, TraceRequestApiVersionV2, &dataquery.TempoQuery{}, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/routing/api/v2/traces/abc123?my_arg=1&span_pruning=true", req.URL.String())
	})

	t.Run("createRequest v2 sends all pruning params when set", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		pruning := true
		groupBy := "db.*,http.method"
		minSpans := int64(10)
		maxParentDepth := int64(-1)
		model := &dataquery.TempoQuery{
			SpanPruning:               &pruning,
			SpanPruningGroupBy:        &groupBy,
			SpanPruningMinSpans:       &minSpans,
			SpanPruningMaxParentDepth: &maxParentDepth,
		}

		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, model, "abc123", 1, 2)
		require.NoError(t, err)

		q := req.URL.Query()
		assert.Equal(t, "true", q.Get("span_pruning"))
		assert.Equal(t, "db.*,http.method", q.Get("span_pruning_group_by"))
		assert.Equal(t, "10", q.Get("span_pruning_min_spans"))
		assert.Equal(t, "-1", q.Get("span_pruning_max_parent_depth"))
		assert.Equal(t, "1", q.Get("start"))
		assert.Equal(t, "2", q.Get("end"))
		assert.Equal(t, 1, strings.Count(req.URL.String(), "?"), "params must be encoded once, not concatenated with a second \"?\"")
	})

	t.Run("createRequest v1 omits pruning params", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		pruning := true
		groupBy := "db.*,http.method"
		minSpans := int64(10)
		maxParentDepth := int64(-1)
		model := &dataquery.TempoQuery{
			SpanPruning:               &pruning,
			SpanPruningGroupBy:        &groupBy,
			SpanPruningMinSpans:       &minSpans,
			SpanPruningMaxParentDepth: &maxParentDepth,
		}

		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV1, model, "abc123", 1, 2)
		require.NoError(t, err)

		// The v1 endpoint rejects unknown params and getTrace falls back to it on a 404.
		assert.Equal(t, "http://tempo/api/traces/abc123?end=2&start=1", req.URL.String())
		assert.NotContains(t, req.URL.String(), "span_pruning")
	})

	t.Run("createRequest v2 sends pruning params with a zero time range", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		pruning := true
		groupBy := "db.*,http.method"
		minSpans := int64(10)
		maxParentDepth := int64(-1)
		model := &dataquery.TempoQuery{
			SpanPruning:               &pruning,
			SpanPruningGroupBy:        &groupBy,
			SpanPruningMinSpans:       &minSpans,
			SpanPruningMaxParentDepth: &maxParentDepth,
		}

		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, model, "abc123", 0, 0)
		require.NoError(t, err)

		// Guards against regressing the url.Values hoist in createRequest: if the read
		// and write-back move back inside the "start != 0 && end != 0" guard, pruning
		// params silently vanish. The frontend zeroes the range by default, so this is
		// the common case, not an edge case.
		q := req.URL.Query()
		assert.Equal(t, "true", q.Get("span_pruning"))
		assert.Equal(t, "db.*,http.method", q.Get("span_pruning_group_by"))
		assert.Equal(t, "10", q.Get("span_pruning_min_spans"))
		assert.Equal(t, "-1", q.Get("span_pruning_max_parent_depth"))
		assert.Empty(t, q.Get("start"))
		assert.Empty(t, q.Get("end"))
	})

	t.Run("createRequest v2 sends span_pruning=false when explicitly disabled", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		pruning := false
		groupBy := "db.*,http.method"
		minSpans := int64(10)
		maxParentDepth := int64(-1)
		model := &dataquery.TempoQuery{
			SpanPruning:               &pruning,
			SpanPruningGroupBy:        &groupBy,
			SpanPruningMinSpans:       &minSpans,
			SpanPruningMaxParentDepth: &maxParentDepth,
		}

		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, model, "abc123", 1, 2)
		require.NoError(t, err)

		// span_pruning=false must be sent explicitly so the request does not depend on
		// the cluster or tenant default; Tempo ignores the sub-params when it is off.
		q := req.URL.Query()
		assert.Equal(t, "false", q.Get("span_pruning"))
		assert.Empty(t, q.Get("span_pruning_group_by"))
		assert.Empty(t, q.Get("span_pruning_min_spans"))
		assert.Empty(t, q.Get("span_pruning_max_parent_depth"))
	})

	t.Run("createRequest v2 omits sub-params when nil", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}

		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, &dataquery.TempoQuery{}, "abc123", 1, 2)
		require.NoError(t, err)

		q := req.URL.Query()
		assert.Equal(t, "true", q.Get("span_pruning"))
		assert.Empty(t, q.Get("span_pruning_group_by"))
		assert.Empty(t, q.Get("span_pruning_min_spans"))
		assert.Empty(t, q.Get("span_pruning_max_parent_depth"))
	})

	t.Run("createRequest v2 transmits a max parent depth of 0", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		maxParentDepth := int64(0)
		model := &dataquery.TempoQuery{SpanPruningMaxParentDepth: &maxParentDepth}

		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, model, "abc123", 1, 2)
		require.NoError(t, err)

		// 0 means "aggregate leaves only" — a truthiness guard would wrongly drop it.
		assert.Equal(t, "0", req.URL.Query().Get("span_pruning_max_parent_depth"))
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
