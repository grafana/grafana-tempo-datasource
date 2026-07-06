package tempo

import (
	"context"
	"net/http"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// outgoingOrgIDs returns the x-scope-orgid values reaching the outgoing metadata.
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

func TestCustomHeadersStreamInterceptor_NoDuplicateOrgID(t *testing.T) {
	opts := httpclient.Options{Header: http.Header{"X-Scope-OrgID": []string{"team-a"}}}
	interceptor := CustomHeadersStreamInterceptor(opts)

	// already present (RunStream path)
	ctxWith := metadata.AppendToOutgoingContext(context.Background(), "X-Scope-OrgID", "team-a")
	if got := outgoingOrgIDs(t, interceptor, ctxWith); len(got) != 1 || got[0] != "team-a" {
		t.Fatalf("header already present: want exactly [team-a], got %d: %v", len(got), got)
	}

	// empty context (CheckHealth path)
	if got := outgoingOrgIDs(t, interceptor, context.Background()); len(got) != 1 || got[0] != "team-a" {
		t.Fatalf("empty context: want exactly [team-a], got %d: %v", len(got), got)
	}
}

func TestCustomHeadersStreamInterceptor_OverwritesConflictingValue(t *testing.T) {
	opts := httpclient.Options{Header: http.Header{"X-Scope-OrgID": []string{"team-a"}}}
	interceptor := CustomHeadersStreamInterceptor(opts)

	ctx := metadata.AppendToOutgoingContext(context.Background(), "X-Scope-OrgID", "stale-tenant")
	if got := outgoingOrgIDs(t, interceptor, ctx); len(got) != 1 || got[0] != "team-a" {
		t.Fatalf("want datasource value to win: exactly [team-a], got %d: %v", len(got), got)
	}
}
