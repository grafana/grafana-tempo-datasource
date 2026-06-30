package tempo

import (
	"context"
	"net/http"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// outgoingOrgIDs runs interceptor over ctx and returns the x-scope-orgid values that
// reach the outgoing metadata when the underlying streamer is finally invoked.
func outgoingOrgIDs(t *testing.T, interceptor grpc.StreamClientInterceptor, ctx context.Context) []string {
	t.Helper()
	var got []string
	streamer := func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		md, _ := metadata.FromOutgoingContext(ctx)
		got = md.Get("x-scope-orgid")
		return nil, nil
	}
	_, _ = interceptor(ctx, &grpc.StreamDesc{}, nil, "/tempopb.StreamingQuerier/Search", streamer)
	return got
}

// TestCustomHeadersStreamInterceptor_NoDuplicateOrgID guards against re-injecting a
// datasource header that RunStream already placed on the outgoing context. A duplicate
// X-Scope-OrgID makes Tempo (dskit) reject the stream with "no org id", because it
// requires exactly one org id value.
func TestCustomHeadersStreamInterceptor_NoDuplicateOrgID(t *testing.T) {
	opts := httpclient.Options{Header: http.Header{"X-Scope-Orgid": []string{"team-a"}}}
	interceptor := CustomHeadersStreamInterceptor(opts)

	// Streaming path: the header is already on the outgoing context (appended by
	// RunStream via GetHeadersFromIncomingContext) -> it must not be added again.
	ctxWith := metadata.AppendToOutgoingContext(context.Background(), "X-Scope-OrgID", "team-a")
	if got := outgoingOrgIDs(t, interceptor, ctxWith); len(got) != 1 || got[0] != "team-a" {
		t.Fatalf("header already present: want exactly [team-a], got %d: %v", len(got), got)
	}

	// Direct path (e.g. CheckHealth): empty outgoing context -> the interceptor must
	// inject the datasource header exactly once.
	if got := outgoingOrgIDs(t, interceptor, context.Background()); len(got) != 1 || got[0] != "team-a" {
		t.Fatalf("empty context: want exactly [team-a], got %d: %v", len(got), got)
	}
}
