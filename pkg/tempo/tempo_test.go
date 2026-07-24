package tempo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryDataUnsupportedQueryType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"traces":[]}`))
	}))
	defer server.Close()

	service := &DataSource{
		info: &DatasourceInfo{
			URL:        server.URL,
			HTTPClient: server.Client(),
		},
		logger: backend.NewLoggerWith("logger", "tempo-test"),
	}

	resp, err := service.QueryData(context.Background(), &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{UID: "tempo-test", Name: "Tempo"},
		},
		Queries: []backend.DataQuery{
			{
				RefID:     "A",
				QueryType: "traceqlMetrics",
				JSON:      json.RawMessage(`{"query":"{} | rate()"}`),
			},
			{
				RefID:     "B",
				QueryType: "traceql",
				JSON:      json.RawMessage(`{"query":"{}"}`),
				TimeRange: backend.TimeRange{From: time.Unix(100, 0), To: time.Unix(200, 0)},
			},
		},
	})

	require.NoError(t, err)

	respA := resp.Responses["A"]
	require.Error(t, respA.Error)
	assert.Contains(t, respA.Error.Error(), "unsupported query type: 'traceqlMetrics' for query with refID 'A'")
	assert.Equal(t, backend.ErrorSourceDownstream, respA.ErrorSource)

	respB := resp.Responses["B"]
	assert.NoError(t, respB.Error)
}

func TestCheckHealth(t *testing.T) {
	tests := []struct {
		name            string
		httpStatusCode  int
		expectedStatus  backend.HealthStatus
		expectedMessage string
	}{
		{
			name:            "successful health check",
			httpStatusCode:  200,
			expectedStatus:  backend.HealthStatusOk,
			expectedMessage: "Data source is working",
		},
		{
			name:            "http error",
			httpStatusCode:  500,
			expectedStatus:  backend.HealthStatusError,
			expectedMessage: "Tempo echo endpoint returned status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.httpStatusCode)
			}))
			defer server.Close()

			pluginCtx := backend.PluginContext{
				DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
					URL: server.URL,
				},
			}

			service := &DataSource{
				info: &DatasourceInfo{
					URL:             server.URL,
					HTTPClient:      server.Client(),
					StreamingClient: nil,
				},
				logger: backend.NewLoggerWith("logger", "tempo-test"),
			}
			ctx := backend.WithPluginContext(context.Background(), pluginCtx)
			result, err := service.CheckHealth(ctx, &backend.CheckHealthRequest{})

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Contains(t, result.Message, tt.expectedMessage)
		})
	}
}
