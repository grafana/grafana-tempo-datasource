package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
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

var diffHeaders = []string{labelPolicyHeader, "Authorization", "X-Custom", "X-Scope-OrgID"}

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
	Hdr map[string]string // header values, joined with a comma
	// Lines are the policy header values as received: one entry per header line over HTTP, one per metadata value over gRPC.
	Lines []string
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

func (p *diffTempo) count(transport string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.rec[transport])
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

func grpcPolicyLines(ctx context.Context) []string {
	md, _ := metadata.FromIncomingContext(ctx)
	return md.Get(labelPolicyMetadataKey)
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
		if !ok {
			t.Errorf("response of type %T is not a proto message", v)
			return
		}
		if err := (&jsonpb.Marshaler{}).Marshal(w, m); err != nil {
			t.Errorf("marshal response: %v", err)
		}
	}

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		p.record("http", diffRecord{Hdr: httpHeaders(r), Lines: r.Header.Values(labelPolicyHeader), Req: diffReq{
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
		p.record("http", diffRecord{Hdr: httpHeaders(r), Lines: r.Header.Values(labelPolicyHeader), Req: diffReq{
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
		p.record("http", diffRecord{Hdr: httpHeaders(r), Lines: r.Header.Values(labelPolicyHeader), Req: diffReq{
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
	s.p.record("grpc", diffRecord{Hdr: hdr, Lines: grpcPolicyLines(srv.Context()), Req: diffReq{
		Kind: "search", Query: req.Query, Limit: req.Limit, Spss: req.SpansPerSpanSet,
		StartNs: int64(req.Start) * int64(time.Second), EndNs: int64(req.End) * int64(time.Second),
	}})
	if req.Query == diffFailQuery {
		return grpcstatus.Error(grpccodes.InvalidArgument, "bad query")
	}
	traces := filterByPolicy(hdr[labelPolicyHeader])
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
	s.p.record("grpc", diffRecord{Hdr: hdr, Lines: grpcPolicyLines(srv.Context()), Req: diffReq{
		Kind: "range", Query: req.Query, StartNs: int64(req.Start), EndNs: int64(req.End), StepNs: int64(req.Step), Exemplars: req.Exemplars,
	}})
	if req.Query == diffFailQuery {
		return grpcstatus.Error(grpccodes.InvalidArgument, "bad query")
	}
	// Like Tempo, each message carries the accumulated series.
	resp := rangeResponse(hdr[labelPolicyHeader])
	for i := 1; i < len(resp.Series); i++ {
		if err := srv.Send(&tempopb.QueryRangeResponse{Series: resp.Series[:i], Metrics: resp.Metrics}); err != nil {
			return err
		}
	}
	return srv.Send(resp)
}

func (s *diffGRPC) MetricsQueryInstant(req *tempopb.QueryInstantRequest, srv tempopb.StreamingQuerier_MetricsQueryInstantServer) error {
	hdr := grpcHeaders(srv.Context())
	s.p.record("grpc", diffRecord{Hdr: hdr, Lines: grpcPolicyLines(srv.Context()), Req: diffReq{
		Kind: "instant", Query: req.Query, StartNs: int64(req.Start), EndNs: int64(req.End),
	}})
	if req.Query == diffFailQuery {
		return grpcstatus.Error(grpccodes.InvalidArgument, "bad query")
	}
	resp := instantResponse(hdr[labelPolicyHeader])
	for i := 1; i < len(resp.Series); i++ {
		if err := srv.Send(&tempopb.QueryInstantResponse{Series: resp.Series[:i], Metrics: resp.Metrics}); err != nil {
			return err
		}
	}
	return srv.Send(resp)
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
	t.Cleanup(grpcSrv.Stop)
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
	return d.runHTTPRange(t, queryJSON, policy, diffRange)
}

func (d diffDS) runHTTPRange(t *testing.T, queryJSON string, policy string, tr backend.TimeRange) backend.DataResponse {
	t.Helper()
	ctx := backend.WithPluginContext(context.Background(), d.pCtx)
	if policy != "" {
		ctx = withHTTPPolicy(ctx, policy)
	}
	resp, err := d.ds.QueryData(ctx, &backend.QueryDataRequest{
		PluginContext: d.pCtx,
		Queries:       []backend.DataQuery{{RefID: "A", QueryType: "traceql", TimeRange: tr, JSON: []byte(queryJSON)}},
	})
	require.NoError(t, err)
	return resp.Responses["A"]
}

// runStream runs the query over the streaming path, with the policy in the incoming gRPC metadata, and returns the
// packets sent to the frontend.
func (d diffDS) runStream(t *testing.T, path, dataJSON, policy string) []json.RawMessage {
	t.Helper()
	return d.runStreamFrom(t, path, dataJSON, policy, "incoming")
}

// runStreamFrom is runStream with the policy in the "incoming" or the "outgoing" gRPC metadata of the context.
func (d diffDS) runStreamFrom(t *testing.T, path, dataJSON, policy, source string) []json.RawMessage {
	t.Helper()
	ctx := config.WithGrafanaConfig(backend.WithPluginContext(context.Background(), d.pCtx),
		config.NewGrafanaCfg(map[string]string{featuretoggles.EnabledFeatures: "streamingForwardTeamHeadersTempo"}))
	if policy != "" {
		var pairs []string
		for _, v := range strings.Split(policy, "|") {
			pairs = append(pairs, labelPolicyMetadataKey, v)
		}
		md := metadata.Pairs(pairs...)
		if source == "outgoing" {
			ctx = metadata.NewOutgoingContext(ctx, md)
		} else {
			ctx = metadata.NewIncomingContext(ctx, md)
		}
	}
	packets := &recordingPacketSender{}
	require.NoError(t, d.ds.RunStream(ctx, &backend.RunStreamRequest{PluginContext: d.pCtx, Path: path, Data: []byte(dataJSON)}, backend.NewStreamSender(packets)))
	return packets.packets
}

// streamData builds the payload the frontend sends over Grafana Live: the query fields plus the time range.
// The keys follow what src/streaming.ts sends (`{...query, SpansPerSpanSet, timeRange}`, and `step` in nanoseconds for
// metrics). The plugin decodes the same payload as the backend query, as TempoQuery and as Tempo's own request, which is
// why the span-set limit appears both as `spss` and as `SpansPerSpanSet`.
func streamData(t *testing.T, query map[string]any) string {
	t.Helper()
	return streamDataRange(t, query, diffRange)
}

// streamDataRange formats the time range like the frontend does (toISOString: UTC, millisecond precision).
func streamDataRange(t *testing.T, query map[string]any, tr backend.TimeRange) string {
	t.Helper()
	const isoMillis = "2006-01-02T15:04:05.000Z"
	withRange := map[string]any{"timeRange": map[string]any{"from": tr.From.UTC().Format(isoMillis), "to": tr.To.UTC().Format(isoMillis)}}
	for k, v := range query {
		withRange[k] = v
	}
	b, err := json.Marshal(withRange)
	require.NoError(t, err)
	return string(b)
}

// diffTrace holds the fields of a trace that both paths expose.
type diffTrace struct {
	ID, Service, Name string
	DurationMs        float64
	StartMs           int64
	// Spans describes the spans of each span set (see spanSignature), span sets separated by "|".
	Spans string
}

// spanSetSignature turns the span IDs of every span set into one comparable string.
func spanSetSignature(sets [][]string) string {
	parts := make([]string, 0, len(sets))
	for _, ids := range sets {
		ids = append([]string(nil), ids...)
		sort.Strings(ids)
		parts = append(parts, strings.Join(ids, ","))
	}
	return strings.Join(parts, "|")
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
			DurationMs: dur, StartMs: col("startTime").At(i).(time.Time).UnixMilli(), Spans: nestedSpans(t, f, i),
		})
	}
	return out
}

// spanSignature describes a span by everything the HTTP table and the streamed result both carry: its ID, name, start
// (ms), duration (ns) and attributes. Attribute values are typed ("s:", "n:", "b:"), integers and doubles both numeric.
func spanSignature(id, name string, startMs, durationNs int64, attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+attrs[k])
	}
	return fmt.Sprintf("%s/%s/%d/%d/%s", id, name, startMs, durationNs, strings.Join(parts, ";"))
}

func numberAttr(v float64) string { return "n:" + strconv.FormatFloat(v, 'g', -1, 64) }

// tableAttr renders a dynamic column value of the HTTP span table; "" means the span has no such attribute.
func tableAttr(v any) string {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() == reflect.Pointer && rv.IsNil()) {
		return ""
	}
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.String:
		return "s:" + rv.String()
	case reflect.Float32, reflect.Float64:
		return numberAttr(rv.Float())
	case reflect.Bool:
		return fmt.Sprintf("b:%t", rv.Bool())
	}
	return fmt.Sprintf("?:%v", rv.Interface())
}

// nestedSpans describes the spans of the span sets nested in a row of the traces table.
func nestedSpans(t *testing.T, f *data.Frame, row int) string {
	t.Helper()
	nested, _ := f.FieldByName("nested")
	if nested == nil {
		return ""
	}
	raw, _ := nested.At(row).(json.RawMessage)
	if len(raw) == 0 {
		return ""
	}
	var frames []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &frames))
	fixed := map[string]bool{"traceIdHidden": true, "spanID": true, "time": true, "name": true, "duration": true}
	var sets [][]string
	for _, fr := range frames {
		var sub data.Frame
		require.NoError(t, json.Unmarshal(fr, &sub))
		col := func(name string) *data.Field {
			fld, _ := sub.FieldByName(name)
			require.NotNil(t, fld, name)
			return fld
		}
		var set []string
		for r := 0; r < sub.Rows(); r++ {
			attrs := map[string]string{}
			for _, fld := range sub.Fields {
				if fixed[fld.Name] {
					continue
				}
				if v := tableAttr(fld.At(r)); v != "" {
					attrs[fld.Name] = v
				}
			}
			set = append(set, spanSignature(col("spanID").At(r).(string), col("name").At(r).(string),
				col("time").At(r).(time.Time).UnixMilli(), int64(col("duration").At(r).(float64)), attrs))
		}
		sets = append(sets, set)
	}
	return spanSetSignature(sets)
}

// streamedAttribute is a span attribute as the plugin marshals it: the value sits in a oneof wrapper.
type streamedAttribute struct {
	Key   string `json:"key"`
	Value struct {
		Value struct {
			String *string  `json:"string_value"`
			Int    *int64   `json:"int_value"`
			Double *float64 `json:"double_value"`
			Bool   *bool    `json:"bool_value"`
		} `json:"Value"`
	} `json:"value"`
}

type streamedSpanSet struct {
	Attributes []streamedAttribute `json:"attributes"`
	Spans      []struct {
		SpanID            string              `json:"spanID"`
		Name              string              `json:"name"`
		StartTimeUnixNano uint64              `json:"startTimeUnixNano"`
		DurationNanos     uint64              `json:"durationNanos"`
		Attributes        []streamedAttribute `json:"attributes"`
	} `json:"spans"`
}

// signatures describes the spans of the set. The HTTP table puts the attributes of the span set on every span first and
// lets the span's own attributes override them.
func (set streamedSpanSet) signatures() []string {
	out := []string{}
	for _, sp := range set.Spans {
		attrs := map[string]string{}
		for _, a := range append(append([]streamedAttribute(nil), set.Attributes...), sp.Attributes...) {
			switch v := a.Value.Value; {
			case v.String != nil:
				attrs[a.Key] = "s:" + *v.String
			case v.Int != nil:
				attrs[a.Key] = numberAttr(float64(*v.Int))
			case v.Double != nil:
				attrs[a.Key] = numberAttr(*v.Double)
			case v.Bool != nil:
				attrs[a.Key] = fmt.Sprintf("b:%t", *v.Bool)
			}
		}
		out = append(out, spanSignature(sp.SpanID, sp.Name, int64(sp.StartTimeUnixNano/1_000_000), int64(sp.DurationNanos), attrs))
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
		TraceID           string            `json:"traceID"`
		RootServiceName   string            `json:"rootServiceName"`
		RootTraceName     string            `json:"rootTraceName"`
		DurationMs        float64           `json:"durationMs"`
		StartTimeUnixNano uint64            `json:"startTimeUnixNano"`
		SpanSet           *streamedSpanSet  `json:"spanSet"`
		SpanSets          []streamedSpanSet `json:"spanSets"`
	}
	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &traces))
	}
	var out []diffTrace
	for _, tr := range traces {
		// The frontend builds the tables from `spanSets` and only falls back to the deprecated `spanSet`
		// (src/resultTransformer.ts), so the streamed side is read the same way.
		var sets [][]string
		if len(tr.SpanSets) > 0 {
			for _, set := range tr.SpanSets {
				sets = append(sets, set.signatures())
			}
		} else if tr.SpanSet != nil {
			sets = append(sets, tr.SpanSet.signatures())
		}
		out = append(out, diffTrace{
			ID: tr.TraceID, Service: tr.RootServiceName, Name: tr.RootTraceName, DurationMs: tr.DurationMs,
			StartMs: int64(tr.StartTimeUnixNano / 1_000_000), Spans: spanSetSignature(sets),
		})
	}
	return out
}

// streamedSpanIDs returns, per streamed trace, the IDs of the spans in its span sets.
func streamedSpanIDs(t *testing.T, packets []json.RawMessage) map[string][]string {
	t.Helper()
	require.NotEmpty(t, packets)
	var packet data.Frame
	require.NoError(t, json.Unmarshal(packets[len(packets)-1], &packet))
	result, _ := packet.FieldByName("result")
	var traces []struct {
		TraceID  string `json:"traceID"`
		SpanSets []struct {
			Spans []struct {
				SpanID string `json:"spanID"`
			} `json:"spans"`
		} `json:"spanSets"`
	}
	require.NoError(t, json.Unmarshal(result.At(0).(json.RawMessage), &traces))
	out := map[string][]string{}
	for _, tr := range traces {
		ids := []string{}
		for _, set := range tr.SpanSets {
			for _, span := range set.Spans {
				ids = append(ids, span.SpanID)
			}
		}
		out[tr.TraceID] = ids
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

	// The policy reaches the plugin as incoming or as outgoing gRPC metadata, depending on how Grafana calls it.
	for _, kind := range []string{"range", "instant"} {
		for _, source := range []string{"incoming", "outgoing"} {
			for _, policy := range policies {
				t.Run("metrics-"+kind+"/"+source+"/policy="+policy, func(t *testing.T) {
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
					packets := d.runStreamFrom(t, MetricsPathPrefix+"differential", streamData(t, streamQuery), policy, source)

					assertSameRequestAndHeaders(t, p, policy)
					require.Equal(t, summarize(t, httpResp.Frames), summarize(t, streamedFrames(t, packets)), "series differ between HTTP and streaming")
				})
			}
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

	require.Equal(t, policy, g.Hdr[labelPolicyHeader])
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

// The streamed traces carry the span sets Tempo sent: the frontend builds the span tables from them, while the HTTP path
// builds the tables in Go, so the two cannot be compared with each other.
func TestDifferential_StreamedSearchKeepsSpanSets(t *testing.T) {
	d := newDiffDS(t, newDiffTempo(t))

	packets := d.runStream(t, SearchPathPrefix+"spansets", streamData(t, map[string]any{"query": "{}", "limit": 7, "spss": 5, "SpansPerSpanSet": 5}), "")

	want := map[string][]string{}
	for _, tr := range parityCorpus {
		ids := []string{}
		for _, set := range tr.SpanSets {
			for _, span := range set.Spans {
				ids = append(ids, span.SpanID)
			}
		}
		want[tr.TraceID] = ids
	}
	require.NotEmpty(t, want["0000000000000001"], "the corpus has no span sets to compare")
	require.Equal(t, want, streamedSpanIDs(t, packets))
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
	// Over HTTP the values arrive as separate header lines, over gRPC as one comma-joined value.
	require.Equal(t, []string{"service=frontend", "service=backend"}, h.Lines)
	require.Equal(t, []string{"service=frontend,service=backend"}, g.Lines)
	// Joined the same way, they are the same header value.
	require.Equal(t, h.Hdr[labelPolicyHeader], g.Hdr[labelPolicyHeader])
}

// A header configured on the datasource wins over the policy Grafana adds, on both paths. This pins the current
// behaviour, not a product requirement: a datasource header named like the policy header overrides it. Over gRPC the
// plugin does this (the interceptor sets datasource headers last). Over HTTP it comes from withHTTPPolicy, which
// mirrors the SDK middleware by adding the policy only when the header is absent; the SDK itself is not exercised.
func TestDifferential_DatasourceHeaderWinsOverTeamPolicy(t *testing.T) {
	p := newDiffTempo(t)
	d := newDiffDSWithPolicy(t, p, "datasource-policy")

	httpResp := d.runHTTP(t, `{"refId":"A","queryType":"traceql","query":"{}"}`, "service=frontend")
	require.NoError(t, httpResp.Error)
	d.runStream(t, SearchPathPrefix+"precedence", streamData(t, map[string]any{"query": "{}"}), "service=frontend")

	require.Equal(t, "datasource-policy", p.only(t, "http").Hdr[labelPolicyHeader])
	require.Equal(t, "datasource-policy", p.only(t, "grpc").Hdr[labelPolicyHeader])
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
			require.Zero(t, p.count("http"), "HTTP request reached Tempo")
			require.Zero(t, p.count("grpc"), "gRPC request reached Tempo")
		})
	}
}
