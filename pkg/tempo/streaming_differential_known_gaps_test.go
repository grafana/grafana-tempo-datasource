package tempo

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/tempo/pkg/tempopb"
	"github.com/stretchr/testify/require"
)

// The differential tests work around two known differences between the HTTP and the streaming path instead of failing
// on them. The tests below assert that each difference still exists, so that when it is fixed they fail and say which
// workaround to remove. They need no real Tempo.

// #314: for a grouped search Tempo sends the first span set as `spanSet` and all of them as `spanSets`. The HTTP table is
// built from `spanSet` when it is there, so it shows only the first span set; the frontend builds one table per span set
// of `spanSets`.
func TestKnownGap_HTTPTableKeepsOnlyTheFirstSpanSet(t *testing.T) {
	pCtx := backend.PluginContext{DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{UID: "u", Name: "n"}}
	span := func(id string) *tempopb.Span {
		return &tempopb.Span{SpanID: id, Name: "op", StartTimeUnixNano: 1_000_000, DurationNanos: 1000}
	}
	first := &tempopb.SpanSet{Matched: 1, Spans: []*tempopb.Span{span("a")}}
	second := &tempopb.SpanSet{Matched: 1, Spans: []*tempopb.Span{span("b")}}
	resp := &tempopb.SearchResponse{Traces: []*tempopb.TraceSearchMetadata{{
		TraceID: "t1", RootServiceName: "svc", RootTraceName: "op", StartTimeUnixNano: 1_000_000, DurationMs: 10,
		SpanSet: first, SpanSets: []*tempopb.SpanSet{first, second},
	}}}

	frames, err := transformTraceSearchResponse(pCtx, resp)
	require.NoError(t, err)
	require.Len(t, frames, 1)
	nested, _ := frames[0].FieldByName("nested")
	require.NotNil(t, nested)
	raw, _ := nested.At(0).(json.RawMessage)
	var subFrames []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &subFrames))

	if len(subFrames) != 1 {
		t.Fatalf("the HTTP table has %d span sets for a trace with two: #314 looks fixed. Remove the known-gap handling for it "+
			"(diffTracesOrGap and firstSpanSet in streaming_differential_generated_test.go), make this test expect two span sets, "+
			"and close the issue.", len(subFrames))
	}
}

// #312: the HTTP path truncates the time range of a metrics query to whole seconds, the streaming path sends nanoseconds.
func TestKnownGap_HTTPTruncatesTheMetricsTimeRange(t *testing.T) {
	tr := backend.TimeRange{
		From: time.Unix(1_000, 0).Add(250 * time.Millisecond),
		To:   time.Unix(1_600, 0).Add(750 * time.Millisecond),
	}
	for _, kind := range []string{"range", "instant"} {
		t.Run(kind, func(t *testing.T) {
			p := newDiffTempo(t)
			d := newDiffDS(t, p)

			httpQuery := map[string]any{"refId": "A", "queryType": "traceql", "query": metricsDiffQuery, "metricsQueryType": kind}
			streamQuery := map[string]any{"query": metricsDiffQuery, "metricsQueryType": kind}
			if kind == "range" {
				httpQuery["step"] = "60s"
				streamQuery["step"] = int64(60 * time.Second)
			}
			httpJSON, err := json.Marshal(httpQuery)
			require.NoError(t, err)

			require.NoError(t, d.runHTTPRange(t, string(httpJSON), "", tr).Error)
			d.runStreamFrom(t, MetricsPathPrefix+"gap", streamDataRange(t, streamQuery, tr), "", "incoming")
			h, g := p.only(t, "http"), p.only(t, "grpc")

			if h.Req.StartNs == g.Req.StartNs && h.Req.EndNs == g.Req.EndNs {
				t.Fatalf("both paths send the same time range for a %s query: #312 looks fixed. Remove the known-gap handling for it "+
					"(requireEqualWithWholeSeconds in streaming_differential_generated_test.go, and the whole-second ranges "+
					"in the other differential tests if you like), delete this test, and close the issue.", kind)
			}
			require.Equal(t, tr.From.Truncate(time.Second).UnixNano(), h.Req.StartNs, "the HTTP path should truncate the start to seconds")
			require.Equal(t, tr.To.Truncate(time.Second).UnixNano(), h.Req.EndNs, "the HTTP path should truncate the end to seconds")
			require.Equal(t, tr.From.UnixNano(), g.Req.StartNs, fmt.Sprintf("the streaming path should send the %s start in nanoseconds", kind))
			require.Equal(t, tr.To.UnixNano(), g.Req.EndNs, fmt.Sprintf("the streaming path should send the %s end in nanoseconds", kind))
		})
	}
}
