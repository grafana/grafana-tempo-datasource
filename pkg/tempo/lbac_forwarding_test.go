package tempo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/stretchr/testify/require"
)

// Every call the plugin makes to Tempo over HTTP must carry the team policy: the SDK adds it from the request context,
// so a request created without that context goes out without it. The differential tests only exercise search and
// metrics; these are the other calls. They have no streaming twin.
func TestTeamPolicyIsForwardedOnEveryHTTPCall(t *testing.T) {
	const policy = "service=frontend"

	var mu sync.Mutex
	policies := map[string][]string{} // request path -> the policy header of each request
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		policies[r.URL.Path] = append(policies[r.URL.Path], strings.Join(r.Header.Values(labelPolicyHeader), ","))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	d := newDiffDS(t, &diffTempo{URL: srv.URL})
	ctx := withHTTPPolicy(backend.WithPluginContext(context.Background(), d.pCtx), policy)

	calls := []struct {
		name string
		path string // part of the path Tempo is expected to receive
		call func()
	}{
		{"trace by ID", "/traces/", func() {
			_, _ = d.ds.QueryData(ctx, &backend.QueryDataRequest{PluginContext: d.pCtx, Queries: []backend.DataQuery{
				{RefID: "A", QueryType: "traceId", JSON: []byte(`{"refId":"A","queryType":"traceId","query":"0000000000000001"}`)},
			}})
		}},
		{"health check", "/api/echo", func() {
			_, _ = d.ds.CheckHealth(ctx, &backend.CheckHealthRequest{PluginContext: d.pCtx})
		}},
		{"tags", "/api/v2/search/tags", func() {
			callResource(t, ctx, d, "tags", "tags")
		}},
		{"tag values", "/api/v2/search/tag/service.name/values", func() {
			callResource(t, ctx, d, "tag-values", "tag-values?tag=service.name")
		}},
	}

	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			mu.Lock()
			policies = map[string][]string{}
			mu.Unlock()

			c.call()

			mu.Lock()
			defer mu.Unlock()
			var matched int
			for path, got := range policies {
				if !strings.Contains(path, c.path) {
					continue
				}
				for _, g := range got {
					matched++
					require.Equal(t, policy, g, "policy header received on %s", path)
				}
			}
			require.NotZero(t, matched, "Tempo received no request for %q; paths: %v", c.path, sortedKeys(policies))
		})
	}
}

// The resource calls (tags, tag values) also copy the headers of the request Grafana sends to the plugin onto the
// request to Tempo, so a policy that arrives there, and not through the context, must be forwarded too.
func TestTeamPolicyInResourceRequestHeadersIsForwarded(t *testing.T) {
	const policy = "service=frontend"

	var mu sync.Mutex
	var got []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, strings.Join(r.Header.Values(labelPolicyHeader), ","))
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	d := newDiffDS(t, &diffTempo{URL: srv.URL})

	err := d.ds.CallResource(backend.WithPluginContext(context.Background(), d.pCtx), &backend.CallResourceRequest{
		PluginContext: d.pCtx, Method: http.MethodGet, Path: "tags", URL: "tags",
		Headers: map[string][]string{labelPolicyHeader: {policy}},
	}, backend.CallResourceResponseSenderFunc(func(*backend.CallResourceResponse) error { return nil }))
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{policy}, got)
}

func callResource(t *testing.T, ctx context.Context, d diffDS, path, url string) {
	t.Helper()
	err := d.ds.CallResource(ctx, &backend.CallResourceRequest{PluginContext: d.pCtx, Method: http.MethodGet, Path: path, URL: url},
		backend.CallResourceResponseSenderFunc(func(*backend.CallResourceResponse) error { return nil }))
	require.NoError(t, err)
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Requests that run at the same time must keep their own policy: the results depend on it, so a policy leaking from one
// request into another changes what the other one gets back.
func TestConcurrentRequestsKeepTheirOwnPolicy(t *testing.T) {
	p := newDiffTempo(t)
	d := newDiffDS(t, p)

	const perPolicy = 12
	policies := []string{"service=frontend", "service=backend", ""}

	type outcome struct {
		policy       string
		http, stream []string
		done         bool // set when both requests ran to the end: a failed require ends a goroutine early
	}
	outcomes := make([]outcome, perPolicy*len(policies))
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			policy := policies[i%len(policies)]
			httpResp := d.runHTTP(t, `{"refId":"A","queryType":"traceql","query":"{}","limit":7,"spss":5,"tableType":"traces"}`, policy)
			packets := d.runStream(t, SearchPathPrefix+"concurrent", streamData(t, map[string]any{"query": "{}", "limit": 7, "spss": 5, "SpansPerSpanSet": 5}), policy)
			outcomes[i] = outcome{policy: policy, http: sortedTraceIDs(httpTraces(t, httpResp.Frames)), stream: sortedTraceIDs(streamedTraces(t, packets)), done: true}
		}()
	}
	wg.Wait()

	for i, o := range outcomes {
		require.True(t, o.done, "request %d did not finish, see the failure logged above", i)
		var want []string
		for _, tr := range filterByPolicy(o.policy) {
			want = append(want, tr.TraceID)
		}
		sort.Strings(want)
		require.Equal(t, want, o.http, "HTTP traces for policy %q", o.policy)
		require.Equal(t, want, o.stream, "streamed traces for policy %q", o.policy)
	}

	for _, transport := range []string{"http", "grpc"} {
		got := map[string]int{}
		p.mu.Lock()
		for _, r := range p.rec[transport] {
			got[r.Hdr["X-Prom-Label-Policy"]]++
		}
		p.mu.Unlock()
		for _, policy := range policies {
			require.Equal(t, perPolicy, got[policy], "requests over %s with policy %q", transport, policy)
		}
	}
}

func sortedTraceIDs(traces []diffTrace) []string {
	ids := make([]string, 0, len(traces))
	for _, tr := range traces {
		ids = append(ids, tr.ID)
	}
	sort.Strings(ids)
	return ids
}
