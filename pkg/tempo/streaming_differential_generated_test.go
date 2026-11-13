package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/stretchr/testify/require"
)

// Differential test against a real Tempo: generate many queries, run each one through the non-streaming (HTTP) and
// the streaming (gRPC) path of the plugin, and compare what comes back. Nothing here knows what a "correct" result
// is, it only checks that the two paths agree.
//
// It needs a Tempo with data (docker compose up -d) and is skipped otherwise:
//
//	DIFF_TEMPO_URL=http://localhost:3200 go test ./pkg/tempo -run TestDifferential_GeneratedQueries -v
//
// DIFF_CASES sets the number of cases per kind (default 60). DIFF_SEED makes a run repeatable; the seed is logged.

var (
	diffFilters = []string{
		`resource.service.name = "app"`, `resource.service.name = "db"`, `resource.service.name = "lb"`, `resource.service.name = "nope"`,
		`span.http.method = "GET"`, `span.http.method = "POST"`, `span.http.status_code = 200`, `span.http.status_code >= 400`,
		`name = "HTTP Client"`, `name =~ "HTTP GET.*"`, `duration > 5ms`, `duration < 50ms`, `status = error`, `status = ok`, `kind = server`, `kind = client`,
	}
	diffGroupBy = []string{"", " by (resource.service.name)", " by (span.http.method)", " by (span.http.status_code)", " by (status)", " by (name)"}
	diffFuncs   = []string{
		"rate()", "count_over_time()", "avg_over_time(duration)", "sum_over_time(duration)", "min_over_time(duration)", "max_over_time(duration)",
		"quantile_over_time(duration, .5, .9)", "histogram_over_time(duration)",
	}
	diffSteps = []string{"", "15s", "30s", "1m", "2m"}

	// Queries Tempo cannot run. Both paths have to fail on them, or both have to cope.
	diffInvalidSearch  = []string{"", " ", "{", "{ span.http.method = }", "not traceql", "{ } }"}
	diffInvalidMetrics = []string{"", " ", "{} | rate(", "{} | nofunc()", "{ } | rate() by ("}
)

// minRecentTraces is how many traces the last few minutes must hold for a run to mean anything.
const minRecentTraces = 20

type diffCase struct {
	Kind       string // search | range | instant
	Query      string
	Limit      int
	Spss       int
	Step       string
	From, To   time.Time
	Fractional bool // the time range has a sub-second part
	Invalid    bool // the query is not valid TraceQL
}

func (c diffCase) String() string {
	return fmt.Sprintf("%s q=%q limit=%d spss=%d step=%q from=%s to=%s", c.Kind, c.Query, c.Limit, c.Spss, c.Step,
		c.From.UTC().Format(time.RFC3339Nano), c.To.UTC().Format(time.RFC3339Nano))
}

func pick[T any](r *rand.Rand, xs []T) T { return xs[r.Intn(len(xs))] }

func genFilter(r *rand.Rand) string {
	switch r.Intn(6) {
	case 0:
		return ""
	case 1:
		return pick(r, diffFilters)
	case 2:
		return pick(r, diffFilters) + " && " + pick(r, diffFilters)
	case 3:
		return pick(r, diffFilters) + " || " + pick(r, diffFilters)
	default:
		return pick(r, diffFilters)
	}
}

func genSearchQuery(r *rand.Rand) string {
	switch r.Intn(8) {
	case 0:
		return "{}"
	case 1:
		return fmt.Sprintf("{ %s } && { %s }", pick(r, diffFilters), pick(r, diffFilters))
	case 2:
		return fmt.Sprintf("{ %s } >> { %s }", pick(r, diffFilters), pick(r, diffFilters))
	case 3:
		return fmt.Sprintf("{ %s } | count() > %d", pick(r, diffFilters), r.Intn(4))
	case 4:
		return fmt.Sprintf("{ %s } | select(span.http.method)", pick(r, diffFilters))
	case 5:
		// Grouping returns several span sets per trace.
		return fmt.Sprintf("{ %s } | by(span.http.method)", pick(r, diffFilters))
	default:
		return fmt.Sprintf("{ %s }", genFilter(r))
	}
}

func genMetricsQuery(r *rand.Rand) string {
	return fmt.Sprintf("{ %s } | %s%s", genFilter(r), pick(r, diffFuncs), pick(r, diffGroupBy))
}

// genRange returns a window that ended 90 to 240 seconds ago, so Tempo has settled it. Fractional windows have a
// millisecond part, like the ones Grafana sends.
func genRange(r *rand.Rand, fractional bool) (time.Time, time.Time) {
	end := time.Now().Add(-time.Duration(90+r.Intn(150)) * time.Second).Truncate(time.Second)
	start := end.Add(-time.Duration(60+r.Intn(540)) * time.Second)
	if fractional {
		ms := time.Duration(1+r.Intn(998)) * time.Millisecond
		return start.Add(ms), end.Add(ms)
	}
	return start, end
}

func genCase(r *rand.Rand, kind string) diffCase {
	c := diffCase{Kind: kind, Fractional: r.Intn(3) == 0}
	c.From, c.To = genRange(r, c.Fractional)
	switch kind {
	case "search":
		c.Query = genSearchQuery(r)
		c.Limit = pick(r, []int{1, 5, 20, 100, 1000})
		c.Spss = 1 + r.Intn(5)
	default:
		c.Query = genMetricsQuery(r)
		if kind == "range" {
			c.Step = pick(r, diffSteps)
		}
	}
	if r.Intn(8) == 0 {
		c.Invalid = true
		if kind == "search" {
			c.Query = pick(r, diffInvalidSearch)
		} else {
			c.Query = pick(r, diffInvalidMetrics)
		}
	}
	return c
}

func stepNanos(step string) int64 {
	if step == "" {
		return 0
	}
	d, _ := time.ParseDuration(step)
	return int64(d)
}

// runDiffCase runs one case over both paths and returns the HTTP result and the streamed packets.
func (d diffDS) runDiffCase(t *testing.T, c diffCase) (backend.DataResponse, []json.RawMessage) {
	t.Helper()
	tr := backend.TimeRange{From: c.From, To: c.To}
	var httpQ, streamQ map[string]any
	var path string
	switch c.Kind {
	case "search":
		httpQ = map[string]any{"refId": "A", "queryType": "traceql", "query": c.Query, "limit": c.Limit, "spss": c.Spss, "tableType": "traces"}
		streamQ = map[string]any{"query": c.Query, "limit": c.Limit, "spss": c.Spss, "SpansPerSpanSet": c.Spss}
		path = SearchPathPrefix + "diff"
	default:
		httpQ = map[string]any{"refId": "A", "queryType": "traceql", "query": c.Query, "metricsQueryType": c.Kind}
		streamQ = map[string]any{"query": c.Query, "metricsQueryType": c.Kind}
		if c.Step != "" {
			httpQ["step"] = c.Step
			streamQ["step"] = stepNanos(c.Step)
		}
		path = MetricsPathPrefix + "diff"
	}
	b, err := json.Marshal(httpQ)
	require.NoError(t, err)
	return d.runHTTPRange(t, string(b), "", tr), d.runStream(t, path, streamDataRange(t, streamQ, tr), "")
}

// streamError returns the error text when the last streamed packet reports an error, and "" otherwise.
func streamError(t *testing.T, packets []json.RawMessage) string {
	t.Helper()
	require.NotEmpty(t, packets)
	var packet data.Frame
	require.NoError(t, json.Unmarshal(packets[len(packets)-1], &packet))
	state, _ := packet.FieldByName("state")
	if state.At(0) != "error" {
		return ""
	}
	errField, _ := packet.FieldByName("error")
	return fmt.Sprint(errField.At(0))
}

// caseErrors returns the error of each path, or "" when it succeeded.
func caseErrors(t *testing.T, httpResp backend.DataResponse, packets []json.RawMessage) (httpErr, streamErr string) {
	t.Helper()
	if httpResp.Error != nil {
		httpErr = httpResp.Error.Error()
	}
	return httpErr, streamError(t, packets)
}

func isTransient(msg string) bool {
	msg = strings.ToLower(msg)
	for _, s := range []string{"timeout", "deadline exceeded", "timed out", "connection reset", "unavailable"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil {
		return v
	}
	return def
}

func TestDifferential_GeneratedQueries_RealTempo(t *testing.T) {
	url := os.Getenv("DIFF_TEMPO_URL")
	if url == "" {
		t.Skip("set DIFF_TEMPO_URL (for example http://localhost:3200) to compare both paths against a real Tempo")
	}
	seed := int64(envInt("DIFF_SEED", int(time.Now().UnixNano()%1_000_000_000)))
	n := envInt("DIFF_CASES", 60)
	t.Logf("seed=%d cases per kind=%d (rerun with DIFF_SEED=%d)", seed, n, seed)
	r := rand.New(rand.NewSource(seed))

	settings := backend.DataSourceInstanceSettings{UID: "tempo", Name: "Tempo", URL: url, JSONData: []byte(`{}`)}
	inst, err := NewDatasource(context.Background(), settings)
	require.NoError(t, err)
	d := diffDS{ds: inst.(*DataSource), pCtx: backend.PluginContext{DataSourceInstanceSettings: &settings}}

	// With no recent traces both paths return nothing and always agree, so check there is data to compare.
	now := time.Now()
	probe := diffCase{Kind: "search", Query: "{}", Limit: 20000, Spss: 3, From: now.Add(-5 * time.Minute).Truncate(time.Second), To: now.Add(-90 * time.Second).Truncate(time.Second)}
	probeResp, _ := d.runDiffCase(t, probe)
	require.NoError(t, probeResp.Error)
	require.GreaterOrEqual(t, len(tracesOf(t, probeResp)), minRecentTraces,
		"Tempo has too few recent traces to compare anything; is the trace generator (loadgen) running?")

	var stats struct{ compared, empty, bothErr, knownGap, spanSetGap int }
	for _, kind := range []string{"search", "range", "instant"} {
		cases := make([]diffCase, 0, n+len(diffInvalidSearch))
		for i := 0; i < n; i++ {
			cases = append(cases, genCase(r, kind))
		}
		// Invalid queries run every time, whatever the seed.
		invalid := diffInvalidSearch
		if kind != "search" {
			invalid = diffInvalidMetrics
		}
		for _, q := range invalid {
			c := genCase(r, kind)
			c.Query, c.Invalid = q, true
			cases = append(cases, c)
		}
		for i, c := range cases {
			ok := t.Run(fmt.Sprintf("%s/%03d", kind, i), func(t *testing.T) {
				httpResp, packets := d.runDiffCase(t, c)
				httpErr, streamErr := caseErrors(t, httpResp, packets)
				// A stalled Tempo can time out one path and not the other. Run the case again before calling that a
				// difference; if it happens again it is one.
				if (httpErr == "") != (streamErr == "") && isTransient(httpErr+streamErr) {
					t.Logf("retrying after a transient error\n  http:   %q\n  stream: %q", httpErr, streamErr)
					httpResp, packets = d.runDiffCase(t, c)
					httpErr, streamErr = caseErrors(t, httpResp, packets)
				}
				if httpErr != "" || streamErr != "" {
					require.True(t, httpErr != "" && streamErr != "", "only one path failed\n  http:   %q\n  stream: %q\n  case: %s", httpErr, streamErr, c)
					stats.bothErr++
					return
				}

				var diff string
				var spanSetGap bool
				if kind == "search" {
					diff, spanSetGap = d.compareSearch(t, c, httpResp, packets)
				} else {
					diff = compareMetrics(t, kind == "range", httpResp.Frames, streamedFrames(t, packets))
				}
				if diff == "" {
					if spanSetGap {
						stats.spanSetGap++
						t.Logf("known gap (several span sets): the HTTP table keeps only the first span set when Tempo sends `spanSet`, streaming keeps all: %s", c)
						return
					}
					stats.compared++
					return
				}
				// The HTTP path truncates the metrics time range to whole seconds, the streaming path doesn't.
				if kind != "search" && c.Fractional {
					stats.knownGap++
					t.Logf("known gap (time range with a sub-second part): %s\n%s", c, diff)
					return
				}
				t.Fatalf("streaming and non-streaming differ\n  case: %s\n%s\nrerun: DIFF_SEED=%d DIFF_CASES=%d", c, diff, seed, n)
			})
			_ = ok
		}
	}
	t.Logf("compared and equal: %d, both failed the same way: %d, known gap (metrics, fractional range): %d, known gap (several span sets): %d", stats.compared, stats.bothErr, stats.knownGap, stats.spanSetGap)
}

// tracesOf reads the traces of a non-streaming search response.
func tracesOf(t *testing.T, resp backend.DataResponse) []diffTrace {
	t.Helper()
	if len(resp.Frames) == 0 || resp.Frames[0].Rows() == 0 {
		return nil
	}
	return httpTraces(t, resp.Frames)
}

// compareSearch returns a description of the difference between the two paths, or "".
// firstSpanSet keeps only the first span set of a trace's span signature.
func firstSpanSet(spans string) string {
	first, _, _ := strings.Cut(spans, "|")
	return first
}

// diffTracesOrGap compares the traces of the two paths. A difference that is only the span sets after the first one,
// which the HTTP table leaves out when Tempo sends `spanSet`, is not returned as a difference but reported as a known gap.
func diffTracesOrGap(http, streamed []diffTrace) (diff string, knownGap bool) {
	diff = diffTraces(http, streamed)
	if diff == "" {
		return "", false
	}
	trimmed := make([]diffTrace, len(streamed))
	for i, tr := range streamed {
		tr.Spans = firstSpanSet(tr.Spans)
		trimmed[i] = tr
	}
	if diffTraces(http, trimmed) == "" {
		return "", true
	}
	return diff, false
}

func (d diffDS) compareSearch(t *testing.T, c diffCase, httpResp backend.DataResponse, packets []json.RawMessage) (string, bool) {
	t.Helper()
	got, streamed := tracesOf(t, httpResp), streamedTraces(t, packets)

	// Without a limit nothing is cut off, so Tempo has to return the same traces over both paths. Each path is compared
	// with the other, not with a reference taken from one of them.
	full := c
	full.Limit = 20000
	fullHTTP, fullPackets := d.runDiffCase(t, full)
	if fullHTTP.Error != nil || streamError(t, fullPackets) != "" {
		return fmt.Sprintf("  the query without a limit failed\n    http:   %v\n    stream: %q", fullHTTP.Error, streamError(t, fullPackets)), false
	}
	universe := tracesOf(t, fullHTTP)
	diff, gap := diffTracesOrGap(universe, streamedTraces(t, fullPackets))
	if diff != "" {
		return "  without a limit:\n" + diff, false
	}

	// With more matches than the limit Tempo returns an arbitrary subset, so two runs can legitimately differ. Then
	// both results have to come from the full result set and respect the limit.
	if len(universe) <= c.Limit {
		diff, limitedGap := diffTracesOrGap(got, streamed)
		return diff, gap || limitedGap
	}
	in := map[string]diffTrace{}
	for _, tr := range universe {
		in[tr.ID] = tr
	}
	for name, res := range map[string][]diffTrace{"http": got, "stream": streamed} {
		if len(res) > c.Limit {
			return fmt.Sprintf("  %s returned %d traces, limit is %d", name, len(res), c.Limit), false
		}
		for _, tr := range res {
			tr.Spans = firstSpanSet(tr.Spans) // the span sets after the first one are the known gap
			want := in[tr.ID]
			want.Spans = firstSpanSet(want.Spans)
			if _, ok := in[tr.ID]; !ok || want != tr {
				return fmt.Sprintf("  %s returned a trace that is not in the full result: %+v", name, tr), false
			}
		}
	}
	return "", gap
}

func diffTraces(a, b []diffTrace) string {
	index := func(xs []diffTrace) map[string]diffTrace {
		m := map[string]diffTrace{}
		for _, x := range xs {
			m[x.ID] = x
		}
		return m
	}
	ma, mb := index(a), index(b)
	var out []string
	if len(a) != len(b) || len(ma) != len(mb) {
		out = append(out, fmt.Sprintf("  http returned %d traces, stream %d", len(a), len(b)))
	}
	for id, x := range ma {
		if y, ok := mb[id]; !ok {
			out = append(out, fmt.Sprintf("  only in http: %+v", x))
		} else if x != y {
			out = append(out, fmt.Sprintf("  differs: http %+v / stream %+v", x, y))
		}
	}
	for id, y := range mb {
		if _, ok := ma[id]; !ok {
			out = append(out, fmt.Sprintf("  only in stream: %+v", y))
		}
	}
	sort.Strings(out)
	if len(out) > 6 {
		out = append(out[:6], fmt.Sprintf("  ... and %d more", len(out)-6))
	}
	return strings.Join(out, "\n")
}

type seriesData struct {
	times  []int64
	values []float64
}

func floatAt(f *data.Field, i int) float64 {
	switch v := f.At(i).(type) {
	case float64:
		return v
	case *float64:
		if v == nil {
			return math.NaN()
		}
		return *v
	}
	return math.NaN()
}

func timeAt(f *data.Field, i int) int64 {
	switch v := f.At(i).(type) {
	case time.Time:
		return v.UnixMilli()
	case *time.Time:
		if v != nil {
			return v.UnixMilli()
		}
	}
	return 0
}

// collectSeries keys every series by name and labels. Instant frames carry the current time, so times are optional.
func collectSeries(frames []*data.Frame, withTimes bool) map[string]seriesData {
	out := map[string]seriesData{}
	for _, fr := range frames {
		if fr.Name == "exemplar" || len(fr.Fields) < 2 {
			continue
		}
		value := fr.Fields[len(fr.Fields)-1]
		s := seriesData{}
		for i := 0; i < value.Len(); i++ {
			s.values = append(s.values, floatAt(value, i))
			if withTimes {
				s.times = append(s.times, timeAt(fr.Fields[0], i))
			}
		}
		out[fr.Name+"|"+value.Labels.String()] = s
	}
	return out
}

func compareMetrics(t *testing.T, withTimes bool, a, b []*data.Frame) string {
	t.Helper()
	ma, mb := collectSeries(a, withTimes), collectSeries(b, withTimes)
	var out []string
	for k, x := range ma {
		y, ok := mb[k]
		if !ok {
			out = append(out, "  series only in http: "+k)
			continue
		}
		if len(x.values) != len(y.values) {
			out = append(out, fmt.Sprintf("  %s: http has %d points, stream %d", k, len(x.values), len(y.values)))
			continue
		}
		for i := range x.values {
			if withTimes && x.times[i] != y.times[i] {
				out = append(out, fmt.Sprintf("  %s: point %d time http %d / stream %d", k, i, x.times[i], y.times[i]))
				break
			}
			xv, yv := x.values[i], y.values[i]
			if math.IsNaN(xv) && math.IsNaN(yv) {
				continue
			}
			if math.Abs(xv-yv) > 1e-9*math.Max(1, math.Max(math.Abs(xv), math.Abs(yv))) {
				out = append(out, fmt.Sprintf("  %s: point %d http %v / stream %v", k, i, xv, yv))
				break
			}
		}
	}
	for k := range mb {
		if _, ok := ma[k]; !ok {
			out = append(out, "  series only in stream: "+k)
		}
	}
	sort.Strings(out)
	if len(out) > 6 {
		out = append(out[:6], fmt.Sprintf("  ... and %d more", len(out)-6))
	}
	return strings.Join(out, "\n")
}
