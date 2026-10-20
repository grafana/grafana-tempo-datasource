package tempo

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/tracing"
	"github.com/grafana/grafana-plugin-sdk-go/config"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/featuretoggles"
	"github.com/grafana/tempo/pkg/tempopb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const labelPolicyHeader = "X-Prom-Label-Policy"

// fakeTempo serves the same label-policy-filtered corpus over HTTP (/api/search) and gRPC (Search), the way a
// Tempo behind a Team LBAC proxy would. It records the policy each transport received.
type fakeTempo struct {
	httpURL string
	grpcURL string

	mu       sync.Mutex
	grpcMD   []metadata.MD
	policies map[string][]string // transport -> received policy values
}

var parityCorpus = []*tempopb.TraceSearchMetadata{
	{TraceID: "0000000000000001", RootServiceName: "frontend", RootTraceName: "GET /", StartTimeUnixNano: 3_000_000_000, DurationMs: 10},
	{TraceID: "0000000000000002", RootServiceName: "backend", RootTraceName: "GET /api", StartTimeUnixNano: 2_000_000_000, DurationMs: 20},
	{TraceID: "0000000000000003", RootServiceName: "frontend", RootTraceName: "POST /login", StartTimeUnixNano: 1_000_000_000, DurationMs: 30},
}

// filterByPolicy applies a `service=<name>` policy; an empty policy returns everything.
func filterByPolicy(policy string) []*tempopb.TraceSearchMetadata {
	want := strings.TrimPrefix(policy, "service=")
	var out []*tempopb.TraceSearchMetadata
	for _, tr := range parityCorpus {
		if policy == "" || tr.RootServiceName == want {
			out = append(out, tr)
		}
	}
	return out
}

func (f *fakeTempo) record(transport, policy string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.policies[transport] = append(f.policies[transport], policy)
}

func (f *fakeTempo) received(transport string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.policies[transport]...)
}

type fakeStreamingQuerier struct {
	tempopb.UnimplementedStreamingQuerierServer
	f *fakeTempo
}

func (s *fakeStreamingQuerier) Search(_ *tempopb.SearchRequest, srv tempopb.StreamingQuerier_SearchServer) error {
	md, _ := metadata.FromIncomingContext(srv.Context())
	policy := strings.Join(md.Get(strings.ToLower(labelPolicyHeader)), ",")
	s.f.record("grpc", policy)
	s.f.mu.Lock()
	s.f.grpcMD = append(s.f.grpcMD, md)
	s.f.mu.Unlock()
	traces := filterByPolicy(policy)
	// Stream one trace per message to exercise the incremental path.
	for _, tr := range traces {
		if err := srv.Send(&tempopb.SearchResponse{Traces: []*tempopb.TraceSearchMetadata{tr}, Metrics: &tempopb.SearchMetrics{}}); err != nil {
			return err
		}
	}
	return nil
}

func newFakeTempo(t *testing.T) *fakeTempo {
	t.Helper()
	f := &fakeTempo{policies: map[string][]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		policy := r.Header.Get(labelPolicyHeader)
		f.record("http", policy)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, (&jsonpb.Marshaler{}).Marshal(w, &tempopb.SearchResponse{
			Traces:  filterByPolicy(policy),
			Metrics: &tempopb.SearchMetrics{},
		}))
	})
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)
	f.httpURL = httpSrv.URL

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	grpcSrv := grpc.NewServer()
	tempopb.RegisterStreamingQuerierServer(grpcSrv, &fakeStreamingQuerier{f: f})
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(grpcSrv.Stop)
	f.grpcURL = lis.Addr().String()

	return f
}

type recordingPacketSender struct {
	mu      sync.Mutex
	packets []json.RawMessage
}

func (s *recordingPacketSender) Send(p *backend.StreamPacket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packets = append(s.packets, p.Data)
	return nil
}

// traceIDs extracts the traceID column from a trace-table frame.
func traceIDs(t *testing.T, frame *data.Frame) []string {
	t.Helper()
	field, _ := frame.FieldByName("traceID")
	require.NotNil(t, field, "frame has no traceID field")
	ids := make([]string, 0, field.Len())
	for i := 0; i < field.Len(); i++ {
		ids = append(ids, field.At(i).(string))
	}
	return ids
}

func newParityDataSource(t *testing.T, fake *fakeTempo) *DataSource {
	t.Helper()
	httpClient, err := httpclient.New(httpclient.Options{})
	require.NoError(t, err)
	conn, err := grpc.NewClient(fake.grpcURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return &DataSource{
		logger: backend.NewLoggerWith("logger", "tsdb.tempo.test"),
		tracer: tracing.DefaultTracer(),
		info: &DatasourceInfo{
			HTTPClient:      httpClient,
			StreamingClient: tempopb.NewStreamingQuerierClient(conn),
			URL:             fake.httpURL,
		},
	}
}

func TestStreamingAndNonStreamingReturnSameResultsWithTeamLBAC(t *testing.T) {
	tests := []struct {
		name   string
		policy string
		want   []string
	}{
		{name: "no policy returns all traces", policy: "", want: []string{"0000000000000001", "0000000000000002", "0000000000000003"}},
		{name: "policy restricts to frontend", policy: "service=frontend", want: []string{"0000000000000001", "0000000000000003"}},
		{name: "policy restricts to backend", policy: "service=backend", want: []string{"0000000000000002"}},
		{name: "policy matching nothing returns no traces", policy: "service=nope", want: []string{}},
	}

	// Where the team header sits when RunStream is called: Grafana can hand it to the plugin as incoming
	// gRPC metadata (API server) or as outgoing metadata.
	for _, source := range []string{"incoming", "outgoing"} {
		for _, tc := range tests {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				fake := newFakeTempo(t)

				ds := newParityDataSource(t, fake)

				pCtx := backend.PluginContext{
					DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{UID: "tempo", Name: "Tempo", URL: fake.httpURL, JSONData: []byte(`{}`)},
				}
				tr := backend.TimeRange{From: time.Unix(0, 0), To: time.Unix(10, 0)}

				// Non-streaming: QueryData -> HTTP. The middleware below stands in for the SDK's header middleware
				// (backend/http_headers.go), which adds a forwarded header only if the request doesn't have it yet.
				qdReq := &backend.QueryDataRequest{
					PluginContext: pCtx,
					Queries: []backend.DataQuery{{
						RefID:     "A",
						QueryType: "traceql",
						TimeRange: tr,
						JSON:      []byte(`{"refId":"A","queryType":"traceql","query":"{}","limit":20}`),
					}},
				}
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
				qdResp, err := ds.QueryData(httpCtx, qdReq)
				require.NoError(t, err)
				require.NoError(t, qdResp.Responses["A"].Error)
				require.NotEmpty(t, qdResp.Responses["A"].Frames)
				httpIDs := traceIDs(t, qdResp.Responses["A"].Frames[0])

				// Streaming: RunStream -> gRPC. Team headers reach the plugin as gRPC metadata.
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
				packets := &recordingPacketSender{}
				streamData, err := json.Marshal(map[string]any{
					"query": "{}",
					"limit": 20,
					"timeRange": map[string]any{
						"from": tr.From.Format(time.RFC3339),
						"to":   tr.To.Format(time.RFC3339),
					},
				})
				require.NoError(t, err)
				err = ds.RunStream(grpcCtx, &backend.RunStreamRequest{
					PluginContext: pCtx,
					Path:          SearchPathPrefix + "parity",
					Data:          streamData,
				}, backend.NewStreamSender(packets))
				require.NoError(t, err)
				require.NotEmpty(t, packets.packets)
				streamIDs := lastStreamedTraceIDs(t, packets.packets)

				require.ElementsMatch(t, tc.want, httpIDs, "non-streaming result")
				require.ElementsMatch(t, tc.want, streamIDs, "streaming result")
				require.Equal(t, []string{tc.policy}, fake.received("http"), "policy received by HTTP")
				require.Equal(t, []string{tc.policy}, fake.received("grpc"), "policy received by gRPC")
			})
		}
	}
}

// lastStreamedTraceIDs reads the final streamed packet, which carries the accumulated trace list.
func lastStreamedTraceIDs(t *testing.T, packets []json.RawMessage) []string {
	t.Helper()
	var frame data.Frame
	require.NoError(t, json.Unmarshal(packets[len(packets)-1], &frame))

	state, _ := frame.FieldByName("state")
	require.Equal(t, "done", state.At(0))
	errField, _ := frame.FieldByName("error")
	require.Empty(t, errField.At(0))

	result, _ := frame.FieldByName("result")
	var traces []struct {
		TraceID string `json:"traceID"`
	}
	raw := result.At(0).(json.RawMessage)
	if len(raw) > 0 { // no traces are sent as null
		require.NoError(t, json.Unmarshal(raw, &traces))
	}
	ids := make([]string, 0, len(traces))
	for _, tr := range traces {
		ids = append(ids, tr.TraceID)
	}
	return ids
}

func TestRunStream_PreservesOutgoingMetadata(t *testing.T) {
	fake := newFakeTempo(t)
	ds := newParityDataSource(t, fake)
	// A datasource header makes RunStream set a header, which would leak into the caller's metadata without a copy.
	pCtx := backend.PluginContext{
		DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
			UID:                     "tempo",
			Name:                    "Tempo",
			URL:                     fake.httpURL,
			JSONData:                []byte(`{"httpHeaderName1":"X-Custom"}`),
			DecryptedSecureJSONData: map[string]string{"httpHeaderValue1": "custom"},
		},
	}

	// Team header forwarding is off, so nothing re-adds the existing metadata and it must survive on its own.
	ctx := backend.WithPluginContext(context.Background(), pCtx)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-other", "kept", "x-prom-label-policy", "service=frontend"))
	before, _ := metadata.FromOutgoingContext(ctx)
	before = before.Copy()

	streamData, err := json.Marshal(map[string]any{
		"query":     "{}",
		"timeRange": map[string]any{"from": time.Unix(0, 0).Format(time.RFC3339), "to": time.Unix(10, 0).Format(time.RFC3339)},
	})
	require.NoError(t, err)
	err = ds.RunStream(ctx, &backend.RunStreamRequest{
		PluginContext: pCtx,
		Path:          SearchPathPrefix + "metadata",
		Data:          streamData,
	}, backend.NewStreamSender(&recordingPacketSender{}))
	require.NoError(t, err)

	require.Len(t, fake.grpcMD, 1)
	require.Equal(t, []string{"kept"}, fake.grpcMD[0].Get("x-other"))
	require.Equal(t, []string{"custom"}, fake.grpcMD[0].Get("x-custom"))
	require.Equal(t, []string{"service=frontend"}, fake.grpcMD[0].Get("x-prom-label-policy"))

	after, _ := metadata.FromOutgoingContext(ctx)
	require.Equal(t, before, after, "RunStream must not mutate the caller's outgoing metadata")
}
