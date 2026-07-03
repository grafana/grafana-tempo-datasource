package tempo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTempo(t *testing.T) {
	t.Run("createRequest v1 without time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV1, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/traces/abc123", req.URL.String())
	})

	t.Run("createRequest v1 with time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV1, "abc123", 1, 2)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/traces/abc123?end=2&start=1", req.URL.String())
	})

	t.Run("createRequest v2 without time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/v2/traces/abc123", req.URL.String())
	})

	t.Run("createRequest v2 with time range - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, "abc123", 1, 2)
		require.NoError(t, err)
		assert.Equal(t, 1, len(req.Header))
		assert.Equal(t, "http://tempo/api/v2/traces/abc123?end=2&start=1", req.URL.String())
	})

	t.Run("createRequest v1 with trailing slash URL - no double slash", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/"}, TraceRequestApiVersionV1, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/traces/abc123", req.URL.String())
	})

	t.Run("createRequest v2 with trailing slash URL - no double slash", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/"}, TraceRequestApiVersionV2, "abc123", 1, 2)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/v2/traces/abc123?end=2&start=1", req.URL.String())
	})

	t.Run("createRequest v2 without trailing slash URL - success", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo"}, TraceRequestApiVersionV2, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/api/v2/traces/abc123", req.URL.String())
	})

	t.Run("createRequest preserves existing query params in the configured URL", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/routing?my_arg=1"}, TraceRequestApiVersionV2, "abc123", 1, 2)
		require.NoError(t, err)
		// The custom my_arg must survive and start/end are appended, not concatenated with a second "?".
		assert.Equal(t, "http://tempo/routing/api/v2/traces/abc123?end=2&my_arg=1&start=1", req.URL.String())
	})

	t.Run("createRequest preserves existing query params without a time range", func(t *testing.T) {
		service := &DataSource{logger: backend.NewLoggerWith("logger", "tempo-test")}
		req, err := service.createRequest(context.Background(), &DatasourceInfo{URL: "http://tempo/routing?my_arg=1"}, TraceRequestApiVersionV2, "abc123", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "http://tempo/routing/api/v2/traces/abc123?my_arg=1", req.URL.String())
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
		query := backend.DataQuery{JSON: []byte(`{"query": "abc123"}`)}

		res, err := service.getTrace(context.Background(), pluginCtx, query)

		assert.True(t, v1Called, "expected v1 endpoint (/api/traces/) to be called")
		assert.Nil(t, res)
		require.Error(t, err)
		assert.True(t, backend.IsDownstreamError(err))
	})
}
