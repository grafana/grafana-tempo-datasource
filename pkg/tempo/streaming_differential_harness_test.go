package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/config"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/featuretoggles"
	"github.com/grafana/tempo/pkg/tempopb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

// This harness checks that, for the same Grafana query, the plugin sends Tempo the same request, the same headers and
// gets back the same result whether it goes over HTTP (non-streaming) or gRPC (streaming).
//
// The datasource is built with NewDatasource, so both clients come from the real settings (TLS, basic auth, custom
// headers). A single TLS server answers HTTP and gRPC and records what each transport received.

var diffHeaders = []string{"X-Prom-Label-Policy", "Authorization", "X-Custom", "X-Scope-OrgID"}

// diffReq is a transport-independent view of the request Tempo received.
type diffReq struct {
	Kind      string // search | range | instant
	Query     string
	Limit     uint32
	Spss      uint32
	StartNs   int64
	EndNs     int64
	StepNs    int64
	Exemplars uint32
}

type diffRecord struct {
	Req diffReq
	Hdr map[string]string
}

type diffTempo struct {
	URL   string
	close func()

	// duplicate makes the gRPC search resend the first trace, like Tempo does when it updates a trace it already sent.
	duplicate bool

	mu  sync.Mutex
	rec map[string][]diffRecord // keyed by transport: http | grpc
}

func (p *diffTempo) record(transport string, r diffRecord) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rec[transport] = append(p.rec[transport], r)
}

func (p *diffTempo) only(t *testing.T, transport string) diffRecord {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.rec[transport], 1, "requests received over %s", transport)
	return p.rec[transport][0]
}

func httpHeaders(r *http.Request) map[string]string {
	out := map[string]string{}
	for _, h := range diffHeaders {
		out[h] = strings.Join(r.Header.Values(h), ",")
	}
	return out
}

func grpcHeaders(ctx context.Context) map[string]string {
	md, _ := metadata.FromIncomingContext(ctx)
	out := map[string]string{}
	for _, h := range diffHeaders {
		out[h] = strings.Join(md.Get(strings.ToLower(h)), ",")
	}
	return out
}

// tempoTime reads a start/end the way Tempo's HTTP API does: up to 10 digits are seconds, more are nanoseconds.
func tempoTime(v string) int64 {
	n, _ := strconv.ParseInt(v, 10, 64)
	if len(v) <= 10 {
		return n * int64(time.Second)
	}
	return n
}

func atoi(v string) uint32 {
	n, _ := strconv.Atoi(v)
	return uint32(n)
}

func (p *diffTempo) handleHTTP(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		m, ok := v.(interface {
			Reset()
			String() string
			ProtoMessage()
		})
		require.True(t, ok)
		require.NoError(t, (&jsonpb.Marshaler{}).Marshal(w, m))
	}

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		p.record("http", diffRecord{Hdr: httpHeaders(r), Req: diffReq{
			Kind: "search", Query: q.Get("q"), Limit: atoi(q.Get("limit")), Spss: atoi(q.Get("spss")),
			StartNs: tempoTime(q.Get("start")), EndNs: tempoTime(q.Get("end")),
		}})
		if q.Get("q") == diffFailQuery {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		reply(w, &tempopb.SearchResponse{Traces: filterByPolicy(r.Header.Get(labelPolicyHeader)), Metrics: &tempopb.SearchMetrics{}})
	})
	mux.HandleFunc("/api/metrics/query_range", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		step, _ := time.ParseDuration(q.Get("step"))
		p.record("http", diffRecord{Hdr: httpHeaders(r), Req: diffReq{
			Kind: "range", Query: q.Get("q"), StartNs: tempoTime(q.Get("start")), EndNs: tempoTime(q.Get("end")),
			StepNs: int64(step), Exemplars: atoi(q.Get("exemplars")),
		}})
		if q.Get("q") == diffFailQuery {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		reply(w, rangeResponse(r.Header.Get(labelPolicyHeader)))
	})
	mux.HandleFunc("/api/metrics/query", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		p.record("http", diffRecord{Hdr: httpHeaders(r), Req: diffReq{
			Kind: "instant", Query: q.Get("q"), StartNs: tempoTime(q.Get("start")), EndNs: tempoTime(q.Get("end")),
		}})
		if q.Get("q") == diffFailQuery {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		reply(w, instantResponse(r.Header.Get(labelPolicyHeader)))
	})
	return mux
}

const diffFailQuery = "{ fail }"

type diffGRPC struct {
	tempopb.UnimplementedStreamingQuerierServer
	p *diffTempo
}

func (s *diffGRPC) Search(req *tempopb.SearchRequest, srv tempopb.StreamingQuerier_SearchServer) error {
	hdr := grpcHeaders(srv.Context())
	s.p.record("grpc", diffRecord{Hdr: hdr, Req: diffReq{
		Kind: "search", Query: req.Query, Limit: req.Limit, Spss: req.SpansPerSpanSet,
		StartNs: int64(req.Start) * int64(time.Second), EndNs: int64(req.End) * int64(time.Second),
	}})
	if req.Query == diffFailQuery {
		return grpcstatus.Error(grpccodes.InvalidArgument, "bad query")
	}
	traces := filterByPolicy(hdr["X-Prom-Label-Policy"])
	if s.p.duplicate && len(traces) > 0 {
		traces = append(traces, traces[0])
	}
	for _, tr := range traces {
		if err := srv.Send(&tempopb.SearchResponse{Traces: []*tempopb.TraceSearchMetadata{tr}, Metrics: &tempopb.SearchMetrics{}}); err != nil {
			return err
		}
	}
	return nil
}

func (s *diffGRPC) MetricsQueryRange(req *tempopb.QueryRangeRequest, srv tempopb.StreamingQuerier_MetricsQueryRangeServer) error {
	hdr := grpcHeaders(srv.Context())
	s.p.record("grpc", diffRecord{Hdr: hdr, Req: diffReq{
		Kind: "range", Query: req.Query, StartNs: int64(req.Start), EndNs: int64(req.End), StepNs: int64(req.Step), Exemplars: req.Exemplars,
	}})
	if req.Query == diffFailQuery {
		return grpcstatus.Error(grpccodes.InvalidArgument, "bad query")
	}
	return srv.Send(rangeResponse(hdr["X-Prom-Label-Policy"]))
}

func (s *diffGRPC) MetricsQueryInstant(req *tempopb.QueryInstantRequest, srv tempopb.StreamingQuerier_MetricsQueryInstantServer) error {
	hdr := grpcHeaders(srv.Context())
	s.p.record("grpc", diffRecord{Hdr: hdr, Req: diffReq{
		Kind: "instant", Query: req.Query, StartNs: int64(req.Start), EndNs: int64(req.End),
	}})
	if req.Query == diffFailQuery {
		return grpcstatus.Error(grpccodes.InvalidArgument, "bad query")
	}
	return srv.Send(instantResponse(hdr["X-Prom-Label-Policy"]))
}

// newDiffTempo starts one TLS server that serves HTTP and gRPC on the same address.
func newDiffTempo(t *testing.T) *diffTempo {
	t.Helper()
	p := &diffTempo{rec: map[string][]diffRecord{}}
	grpcSrv := grpc.NewServer()
	tempopb.RegisterStreamingQuerierServer(grpcSrv, &diffGRPC{p: p})
	httpHandler := p.handleHTTP(t)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcSrv.ServeHTTP(w, r)
			return
		}
		httpHandler.ServeHTTP(w, r)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	p.URL = srv.URL
	p.close = srv.Close
	return p
}

type diffDS struct {
	ds   *DataSource
	pCtx backend.PluginContext
}

// newDiffDS builds the datasource the way Grafana does, with basic auth and two custom headers configured.
func newDiffDS(t *testing.T, p *diffTempo) diffDS {
	t.Helper()
	return newDiffDSWithPolicy(t, p, "")
}

// newDiffDSWithPolicy also configures a datasource header named like the team policy header, when value is set.
func newDiffDSWithPolicy(t *testing.T, p *diffTempo, datasourcePolicy string) diffDS {
	t.Helper()
	jsonData := `{"tlsSkipVerify":true,"httpHeaderName1":"X-Custom","httpHeaderName2":"X-Scope-OrgID"}`
	secure := map[string]string{"basicAuthPassword": "pw", "httpHeaderValue1": "custom", "httpHeaderValue2": "org-1"}
	if datasourcePolicy != "" {
		jsonData = `{"tlsSkipVerify":true,"httpHeaderName1":"X-Custom","httpHeaderName2":"X-Scope-OrgID","httpHeaderName3":"X-Prom-Label-Policy"}`
		secure["httpHeaderValue3"] = datasourcePolicy
	}
	settings := backend.DataSourceInstanceSettings{
		UID:                     "tempo",
		Name:                    "Tempo",
		URL:                     p.URL,
		BasicAuthEnabled:        true,
		BasicAuthUser:           "user",
		JSONData:                []byte(jsonData),
		DecryptedSecureJSONData: secure,
	}
	inst, err := NewDatasource(context.Background(), settings)
	require.NoError(t, err)
	return diffDS{ds: inst.(*DataSource), pCtx: backend.PluginContext{DataSourceInstanceSettings: &settings}}
}

// A whole-second range: the HTTP path truncates the time range to seconds and the streaming path does not for metrics.
var diffRange = backend.TimeRange{From: time.Unix(1_000, 0), To: time.Unix(1_600, 0)}

// withHTTPPolicy stands in for the SDK's header middleware (backend/http_headers.go): it adds the policy to the HTTP
// requests made with ctx, only when the client was created with ForwardHTTPHeaders. Several values are separated by "|".
func withHTTPPolicy(ctx context.Context, policy string) context.Context {
	return httpclient.WithContextualMiddleware(ctx, httpclient.NamedMiddlewareFunc("lbac", func(opts httpclient.Options, next http.RoundTripper) http.RoundTripper {
		if !opts.ForwardHTTPHeaders {
			return next
		}
		return httpclient.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get(labelPolicyHeader) == "" {
				for _, v := range strings.Split(policy, "|") {
					r.Header.Add(labelPolicyHeader, v)
				}
			}
			return next.RoundTrip(r)
		})
	}))
}

// runHTTP runs the query over the non-streaming path.
func (d diffDS) runHTTP(t *testing.T, queryJSON string, policy string) backend.DataResponse {
	t.Helper()
	ctx := backend.WithPluginContext(context.Background(), d.pCtx)
	if policy != "" {
		ctx = withHTTPPolicy(ctx, policy)
	}
	resp, err := d.ds.QueryData(ctx, &backend.QueryDataRequest{
		PluginContext: d.pCtx,
		Queries:       []backend.DataQuery{{RefID: "A", QueryType: "traceql", TimeRange: diffRange, JSON: []byte(queryJSON)}},
	})
	require.NoError(t, err)
	return resp.Responses["A"]
}

// runStream runs the query over the streaming path and returns the packets sent to the frontend and the error, if any.
func (d diffDS) runStream(t *testing.T, path, dataJSON, policy string) []json.RawMessage {
	t.Helper()
	ctx := config.WithGrafanaConfig(backend.WithPluginContext(context.Background(), d.pCtx),
		config.NewGrafanaCfg(map[string]string{featuretoggles.EnabledFeatures: "streamingForwardTeamHeadersTempo"}))
	if policy != "" {
		var pairs []string
		for _, v := range strings.Split(policy, "|") {
			pairs = append(pairs, "x-prom-label-policy", v)
		}
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(pairs...))
	}
	packets := &recordingPacketSender{}
	require.NoError(t, d.ds.RunStream(ctx, &backend.RunStreamRequest{PluginContext: d.pCtx, Path: path, Data: []byte(dataJSON)}, backend.NewStreamSender(packets)))
	return packets.packets
}

// streamData builds the payload the frontend sends over Grafana Live: the query fields plus the time range.
func streamData(t *testing.T, query map[string]any) string {
	t.Helper()
	query["timeRange"] = map[string]any{"from": diffRange.From.Format(time.RFC3339), "to": diffRange.To.Format(time.RFC3339)}
	b, err := json.Marshal(query)
	require.NoError(t, err)
	return string(b)
}

// diffTrace holds the fields of a trace that both paths expose.
type diffTrace struct {
	ID, Service, Name string
	DurationMs        float64
	StartMs           int64
}

func httpTraces(t *testing.T, frames []*data.Frame) []diffTrace {
	t.Helper()
	require.NotEmpty(t, frames)
	f := frames[0]
	col := func(name string) *data.Field {
		fld, _ := f.FieldByName(name)
		require.NotNil(t, fld, name)
		return fld
	}
	var out []diffTrace
	for i := 0; i < f.Rows(); i++ {
		var dur float64
		if v := col("traceDuration").At(i).(*float64); v != nil {
			dur = *v
		}
		out = append(out, diffTrace{
			ID: col("traceID").At(i).(string), Service: col("traceService").At(i).(string), Name: col("traceName").At(i).(string),
			DurationMs: dur, StartMs: col("startTime").At(i).(time.Time).UnixMilli(),
		})
	}
	return out
}

func streamedTraces(t *testing.T, packets []json.RawMessage) []diffTrace {
	t.Helper()
	require.NotEmpty(t, packets)
	var packet data.Frame
	require.NoError(t, json.Unmarshal(packets[len(packets)-1], &packet))
	state, _ := packet.FieldByName("state")
	require.Equal(t, "done", state.At(0))
	result, _ := packet.FieldByName("result")
	raw := result.At(0).(json.RawMessage)
	var traces []struct {
		TraceID           string  `json:"traceID"`
		RootServiceName   string  `json:"rootServiceName"`
		RootTraceName     string  `json:"rootTraceName"`
		DurationMs        float64 `json:"durationMs"`
		StartTimeUnixNano uint64  `json:"startTimeUnixNano"`
	}
	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &traces))
	}
	var out []diffTrace
	for _, tr := range traces {
		out = append(out, diffTrace{ID: tr.TraceID, Service: tr.RootServiceName, Name: tr.RootTraceName, DurationMs: tr.DurationMs, StartMs: int64(tr.StartTimeUnixNano / 1_000_000)})
	}
	return out
}

// lastPacketError returns the error text of the last streamed packet.
func lastPacketError(t *testing.T, packets []json.RawMessage) string {
	t.Helper()
	require.NotEmpty(t, packets)
	var packet data.Frame
	require.NoError(t, json.Unmarshal(packets[len(packets)-1], &packet))
	state, _ := packet.FieldByName("state")
	errField, _ := packet.FieldByName("error")
	require.Equal(t, "error", state.At(0))
	return fmt.Sprint(errField.At(0))
}

func TestDifferential_RequestsHeadersAndResults(t *testing.T) {
	policies := []string{"", "service=frontend", "service=nope"}

	for _, policy := range policies {
		t.Run("search/policy="+policy, func(t *testing.T) {
			p := newDiffTempo(t)
			p.duplicate = true
			d := newDiffDS(t, p)

			httpResp := d.runHTTP(t, `{"refId":"A","queryType":"traceql","query":"{ span.http.method = \"GET\" }","limit":7,"spss":5,"tableType":"traces"}`, policy)
			require.NoError(t, httpResp.Error)
			packets := d.runStream(t, SearchPathPrefix+"differential", streamData(t, map[string]any{
				"query": `{ span.http.method = "GET" }`, "limit": 7, "spss": 5, "SpansPerSpanSet": 5,
			}), policy)

			assertSameRequestAndHeaders(t, p, policy)
			require.ElementsMatch(t, httpTraces(t, httpResp.Frames), streamedTraces(t, packets), "traces differ between HTTP and streaming")
		})
	}

	for _, kind := range []string{"range", "instant"} {
		for _, policy := range policies {
			t.Run("metrics-"+kind+"/policy="+policy, func(t *testing.T) {
				p := newDiffTempo(t)
				d := newDiffDS(t, p)

				httpQuery := map[string]any{"refId": "A", "queryType": "traceql", "query": metricsDiffQuery, "metricsQueryType": kind}
				streamQuery := map[string]any{"query": metricsDiffQuery, "metricsQueryType": kind}
				if kind == "range" {
					httpQuery["step"], httpQuery["exemplars"] = "60s", 100
					streamQuery["step"], streamQuery["exemplars"] = int64(60*time.Second), 100
				}
				httpJSON, err := json.Marshal(httpQuery)
				require.NoError(t, err)

				httpResp := d.runHTTP(t, string(httpJSON), policy)
				require.NoError(t, httpResp.Error)
				packets := d.runStream(t, MetricsPathPrefix+"differential", streamData(t, streamQuery), policy)

				assertSameRequestAndHeaders(t, p, policy)
				require.Equal(t, summarize(t, httpResp.Frames), summarize(t, streamedFrames(t, packets)), "series differ between HTTP and streaming")
			})
		}
	}
}

// assertSameRequestAndHeaders checks that Tempo received the same request and headers over both transports, and that
// the policy and the datasource credentials arrived.
func assertSameRequestAndHeaders(t *testing.T, p *diffTempo, policy string) {
	t.Helper()
	h, g := p.only(t, "http"), p.only(t, "grpc")
	require.Equal(t, h.Req, g.Req, "request differs between HTTP and streaming")
	require.Equal(t, h.Hdr, g.Hdr, "headers differ between HTTP and streaming")

	require.Equal(t, policy, g.Hdr["X-Prom-Label-Policy"])
	require.Equal(t, "custom", g.Hdr["X-Custom"])
	require.Equal(t, "org-1", g.Hdr["X-Scope-OrgID"])
	require.True(t, strings.HasPrefix(g.Hdr["Authorization"], "Basic "), "basic auth missing: %q", g.Hdr["Authorization"])
}

func TestDifferential_QueryErrors(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		d := newDiffDS(t, newDiffTempo(t))
		httpResp := d.runHTTP(t, fmt.Sprintf(`{"refId":"A","queryType":"traceql","query":%q}`, diffFailQuery), "")
		require.Error(t, httpResp.Error)
		require.NotEmpty(t, lastPacketError(t, d.runStream(t, SearchPathPrefix+"err", streamData(t, map[string]any{"query": diffFailQuery}), "")))
	})
	for _, kind := range []string{"range", "instant"} {
		t.Run("metrics-"+kind, func(t *testing.T) {
			d := newDiffDS(t, newDiffTempo(t))
			httpResp := d.runHTTP(t, fmt.Sprintf(`{"refId":"A","queryType":"traceql","query":%q,"metricsQueryType":%q}`, diffFailQuery, kind), "")
			require.Error(t, httpResp.Error)
			require.NotEmpty(t, lastPacketError(t, d.runStream(t, MetricsPathPrefix+"err", streamData(t, map[string]any{"query": diffFailQuery, "metricsQueryType": kind}), "")))
		})
	}
}

// A policy with several values (one per team): HTTP sends one header line per value, the streaming path joins them with
// a comma. Joined the same way, Tempo has to see the same value.
func TestDifferential_MultiValuePolicy(t *testing.T) {
	p := newDiffTempo(t)
	d := newDiffDS(t, p)
	const policy = "service=frontend|service=backend"

	httpResp := d.runHTTP(t, `{"refId":"A","queryType":"traceql","query":"{}","limit":7,"spss":5}`, policy)
	require.NoError(t, httpResp.Error)
	d.runStream(t, SearchPathPrefix+"multi", streamData(t, map[string]any{"query": "{}", "limit": 7, "spss": 5, "SpansPerSpanSet": 5}), policy)

	h, g := p.only(t, "http"), p.only(t, "grpc")
	require.Equal(t, "service=frontend,service=backend", h.Hdr["X-Prom-Label-Policy"])
	require.Equal(t, h.Hdr["X-Prom-Label-Policy"], g.Hdr["X-Prom-Label-Policy"])
}

// A header configured on the datasource wins over the policy Grafana adds, on both paths.
func TestDifferential_DatasourceHeaderWinsOverTeamPolicy(t *testing.T) {
	p := newDiffTempo(t)
	d := newDiffDSWithPolicy(t, p, "datasource-policy")

	httpResp := d.runHTTP(t, `{"refId":"A","queryType":"traceql","query":"{}"}`, "service=frontend")
	require.NoError(t, httpResp.Error)
	d.runStream(t, SearchPathPrefix+"precedence", streamData(t, map[string]any{"query": "{}"}), "service=frontend")

	require.Equal(t, "datasource-policy", p.only(t, "http").Hdr["X-Prom-Label-Policy"])
	require.Equal(t, "datasource-policy", p.only(t, "grpc").Hdr["X-Prom-Label-Policy"])
}

// With no limit, spans per span set, step or exemplars in the query, both paths must send the same (absent) values.
func TestDifferential_DefaultsWhenOptionalFieldsAbsent(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		p := newDiffTempo(t)
		d := newDiffDS(t, p)
		require.NoError(t, d.runHTTP(t, `{"refId":"A","queryType":"traceql","query":"{}"}`, "").Error)
		d.runStream(t, SearchPathPrefix+"defaults", streamData(t, map[string]any{"query": "{}"}), "")
		require.Equal(t, p.only(t, "http").Req, p.only(t, "grpc").Req)
	})
	t.Run("metrics-range", func(t *testing.T) {
		p := newDiffTempo(t)
		d := newDiffDS(t, p)
		require.NoError(t, d.runHTTP(t, fmt.Sprintf(`{"refId":"A","queryType":"traceql","query":%q,"metricsQueryType":"range"}`, metricsDiffQuery), "").Error)
		d.runStream(t, MetricsPathPrefix+"defaults", streamData(t, map[string]any{"query": metricsDiffQuery, "metricsQueryType": "range"}), "")
		require.Equal(t, p.only(t, "http").Req, p.only(t, "grpc").Req)
	})
}

// When Tempo cannot be reached, both paths must report an error.
func TestDifferential_TempoUnreachable(t *testing.T) {
	for _, flow := range []string{"search", "metrics-range"} {
		t.Run(flow, func(t *testing.T) {
			p := newDiffTempo(t)
			d := newDiffDS(t, p)
			// Closing the server makes every request fail.
			p.close()

			httpJSON := `{"refId":"A","queryType":"traceql","query":"{}"}`
			path, streamQuery := SearchPathPrefix+"down", map[string]any{"query": "{}"}
			if flow == "metrics-range" {
				httpJSON = fmt.Sprintf(`{"refId":"A","queryType":"traceql","query":%q,"metricsQueryType":"range"}`, metricsDiffQuery)
				path, streamQuery = MetricsPathPrefix+"down", map[string]any{"query": metricsDiffQuery, "metricsQueryType": "range"}
			}
			require.Error(t, d.runHTTP(t, httpJSON, "").Error)
			require.NotEmpty(t, lastPacketError(t, d.runStream(t, path, streamData(t, streamQuery), "")))
		})
	}
}

// An empty query is rejected on both paths, before Tempo is called.
func TestDifferential_EmptyQueryRejected(t *testing.T) {
	for _, flow := range []string{"search", "metrics-range"} {
		t.Run(flow, func(t *testing.T) {
			p := newDiffTempo(t)
			d := newDiffDS(t, p)
			httpJSON, path, streamQuery := `{"refId":"A","queryType":"traceql","query":""}`, SearchPathPrefix+"empty", map[string]any{"query": ""}
			if flow == "metrics-range" {
				httpJSON = `{"refId":"A","queryType":"traceql","query":"","metricsQueryType":"range"}`
				path, streamQuery = MetricsPathPrefix+"empty", map[string]any{"query": "", "metricsQueryType": "range"}
			}
			require.Error(t, d.runHTTP(t, httpJSON, "").Error)
			require.NotEmpty(t, lastPacketError(t, d.runStream(t, path, streamData(t, streamQuery), "")))
			require.Empty(t, p.rec["http"], "HTTP request reached Tempo")
			require.Empty(t, p.rec["grpc"], "gRPC request reached Tempo")
		})
	}
}
