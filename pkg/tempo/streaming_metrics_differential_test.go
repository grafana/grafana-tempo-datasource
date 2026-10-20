package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/config"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/featuretoggles"
	"github.com/grafana/tempo/pkg/tempopb"
	v1 "github.com/grafana/tempo/pkg/tempopb/common/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const metricsDiffQuery = `{} | rate() by (resource.service.name)`

var metricsDiffServices = map[string]float64{"frontend": 1.5, "backend": 2.5}

func filterServices(policy string) []string {
	want := strings.TrimPrefix(policy, "service=")
	var out []string
	for svc := range metricsDiffServices {
		if policy == "" || svc == want {
			out = append(out, svc)
		}
	}
	sort.Strings(out)
	return out
}

func serviceLabels(svc string) []v1.KeyValue {
	return []v1.KeyValue{{Key: "resource.service.name", Value: &v1.AnyValue{Value: &v1.AnyValue_StringValue{StringValue: svc}}}}
}

func rangeResponse(policy string) *tempopb.QueryRangeResponse {
	resp := &tempopb.QueryRangeResponse{Metrics: &tempopb.SearchMetrics{}}
	for _, svc := range filterServices(policy) {
		resp.Series = append(resp.Series, &tempopb.TimeSeries{
			Labels: serviceLabels(svc),
			Samples: []tempopb.Sample{
				{TimestampMs: 1_000, Value: metricsDiffServices[svc]},
				{TimestampMs: 2_000, Value: metricsDiffServices[svc] + 1},
			},
		})
	}
	return resp
}

func instantResponse(policy string) *tempopb.QueryInstantResponse {
	resp := &tempopb.QueryInstantResponse{Metrics: &tempopb.SearchMetrics{}}
	for _, svc := range filterServices(policy) {
		resp.Series = append(resp.Series, &tempopb.InstantSeries{Labels: serviceLabels(svc), Value: metricsDiffServices[svc]})
	}
	return resp
}

type fakeMetricsQuerier struct {
	tempopb.UnimplementedStreamingQuerierServer
	f *fakeTempo
}

func (s *fakeMetricsQuerier) policy(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	policy := strings.Join(md.Get(strings.ToLower(labelPolicyHeader)), ",")
	s.f.record("grpc", policy)
	return policy
}

func (s *fakeMetricsQuerier) MetricsQueryRange(_ *tempopb.QueryRangeRequest, srv tempopb.StreamingQuerier_MetricsQueryRangeServer) error {
	return srv.Send(rangeResponse(s.policy(srv.Context())))
}

func (s *fakeMetricsQuerier) MetricsQueryInstant(_ *tempopb.QueryInstantRequest, srv tempopb.StreamingQuerier_MetricsQueryInstantServer) error {
	return srv.Send(instantResponse(s.policy(srv.Context())))
}

// newFakeMetricsTempo serves the same policy-filtered metrics over HTTP and gRPC.
func newFakeMetricsTempo(t *testing.T) *fakeTempo {
	t.Helper()
	f := &fakeTempo{policies: map[string][]string{}}

	mux := http.NewServeMux()
	serve := func(msg func(policy string) any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			policy := r.Header.Get(labelPolicyHeader)
			f.record("http", policy)
			w.Header().Set("Content-Type", "application/json")
			m, ok := msg(policy).(interface {
				Reset()
				String() string
				ProtoMessage()
			})
			require.True(t, ok)
			require.NoError(t, (&jsonpb.Marshaler{}).Marshal(w, m))
		}
	}
	mux.HandleFunc("/api/metrics/query_range", serve(func(p string) any { return rangeResponse(p) }))
	mux.HandleFunc("/api/metrics/query", serve(func(p string) any { return instantResponse(p) }))
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)
	f.httpURL = httpSrv.URL

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	grpcSrv := grpc.NewServer()
	tempopb.RegisterStreamingQuerierServer(grpcSrv, &fakeMetricsQuerier{f: f})
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(grpcSrv.Stop)
	f.grpcURL = lis.Addr().String()
	return f
}

// summarize reduces frames to a comparable form. The instant frame's time column is time.Now(), so it is left out.
func summarize(t *testing.T, frames []*data.Frame) []string {
	t.Helper()
	out := make([]string, 0, len(frames))
	for _, fr := range frames {
		value := fr.Fields[len(fr.Fields)-1]
		vals := make([]float64, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			vals = append(vals, value.At(i).(float64))
		}
		var times []int64
		if value.Len() > 1 {
			for i := 0; i < fr.Fields[0].Len(); i++ {
				times = append(times, fr.Fields[0].At(i).(time.Time).UnixMilli())
			}
		}
		out = append(out, fmt.Sprintf("%s|%s|%v|%v", fr.Name, value.Labels, vals, times))
	}
	sort.Strings(out)
	return out
}

// streamedFrames reads the last streamed packet (state done), which carries the final frames.
func streamedFrames(t *testing.T, packets []json.RawMessage) []*data.Frame {
	t.Helper()
	require.NotEmpty(t, packets)
	var packet data.Frame
	require.NoError(t, json.Unmarshal(packets[len(packets)-1], &packet))

	state, _ := packet.FieldByName("state")
	require.Equal(t, "done", state.At(0))
	errField, _ := packet.FieldByName("error")
	require.Empty(t, errField.At(0))

	result, _ := packet.FieldByName("result")
	raw := result.At(0).(json.RawMessage)
	if len(raw) == 0 {
		return nil
	}
	var frames []*data.Frame
	require.NoError(t, json.Unmarshal(raw, &frames))
	return frames
}

func TestStreamingAndNonStreamingMetricsReturnSameResultsWithTeamLBAC(t *testing.T) {
	policies := []struct {
		name   string
		policy string
		want   []string
	}{
		{name: "no policy", policy: "", want: []string{"backend", "frontend"}},
		{name: "policy restricts to frontend", policy: "service=frontend", want: []string{"frontend"}},
		{name: "policy matching nothing", policy: "service=nope"},
	}

	for _, queryType := range []string{"range", "instant"} {
		for _, source := range []string{"incoming", "outgoing"} {
			for _, tc := range policies {
				t.Run(queryType+"/"+source+"/"+tc.name, func(t *testing.T) {
					fake := newFakeMetricsTempo(t)
					ds := newParityDataSource(t, fake)
					pCtx := backend.PluginContext{
						DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{UID: "tempo", Name: "Tempo", URL: fake.httpURL, JSONData: []byte(`{}`)},
					}
					tr := backend.TimeRange{From: time.Unix(0, 0), To: time.Unix(10, 0)}

					// Non-streaming: QueryData -> HTTP. The middleware stands in for the SDK's header middleware.
					httpCtx := backend.WithPluginContext(context.Background(), pCtx)
					if tc.policy != "" {
						httpCtx = httpclient.WithContextualMiddleware(httpCtx, httpclient.NamedMiddlewareFunc("lbac", func(_ httpclient.Options, next http.RoundTripper) http.RoundTripper {
							return httpclient.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
								if r.Header.Get(labelPolicyHeader) == "" {
									r.Header.Add(labelPolicyHeader, tc.policy)
								}
								return next.RoundTrip(r)
							})
						}))
					}
					qdResp, err := ds.QueryData(httpCtx, &backend.QueryDataRequest{
						PluginContext: pCtx,
						Queries: []backend.DataQuery{{
							RefID:     "A",
							QueryType: "traceql",
							TimeRange: tr,
							JSON:      []byte(fmt.Sprintf(`{"refId":"A","queryType":"traceql","query":%q,"metricsQueryType":%q,"step":"1s"}`, metricsDiffQuery, queryType)),
						}},
					})
					require.NoError(t, err)
					require.NoError(t, qdResp.Responses["A"].Error)
					httpFrames := qdResp.Responses["A"].Frames

					// Streaming: RunStream -> gRPC.
					grpcCtx := config.WithGrafanaConfig(backend.WithPluginContext(context.Background(), pCtx),
						config.NewGrafanaCfg(map[string]string{featuretoggles.EnabledFeatures: "streamingForwardTeamHeadersTempo"}))
					if tc.policy != "" {
						md := metadata.Pairs("x-prom-label-policy", tc.policy)
						if source == "incoming" {
							grpcCtx = metadata.NewIncomingContext(grpcCtx, md)
						} else {
							grpcCtx = metadata.NewOutgoingContext(grpcCtx, md)
						}
					}
					streamData, err := json.Marshal(map[string]any{
						"query":            metricsDiffQuery,
						"metricsQueryType": queryType,
						"timeRange":        map[string]any{"from": tr.From.Format(time.RFC3339), "to": tr.To.Format(time.RFC3339)},
					})
					require.NoError(t, err)
					packets := &recordingPacketSender{}
					require.NoError(t, ds.RunStream(grpcCtx, &backend.RunStreamRequest{
						PluginContext: pCtx,
						Path:          MetricsPathPrefix + "differential",
						Data:          streamData,
					}, backend.NewStreamSender(packets)))
					streamFrames := streamedFrames(t, packets.packets)

					require.Len(t, httpFrames, len(tc.want), "non-streaming frames")
					require.Equal(t, summarize(t, httpFrames), summarize(t, streamFrames), "streaming and non-streaming frames differ")
					require.Equal(t, []string{tc.policy}, fake.received("http"), "policy received by HTTP")
					require.Equal(t, []string{tc.policy}, fake.received("grpc"), "policy received by gRPC")
				})
			}
		}
	}
}
