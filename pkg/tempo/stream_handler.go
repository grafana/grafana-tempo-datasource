package tempo

import (
	"context"
	"fmt"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	stream_utils "github.com/grafana/grafana-tempo-datasource/pkg/tempo/utils"
	"google.golang.org/grpc/metadata"
)

func (ds *DataSource) SubscribeStream(_ context.Context, req *backend.SubscribeStreamRequest) (*backend.SubscribeStreamResponse, error) {
	ds.logger.Debug("Allowing access to stream", "path", req.Path, "user", req.PluginContext.User)

	if strings.HasPrefix(req.Path, SearchPathPrefix) {
		return &backend.SubscribeStreamResponse{
			Status: backend.SubscribeStreamStatusOK,
		}, nil
	}

	if strings.HasPrefix(req.Path, MetricsPathPrefix) {
		return &backend.SubscribeStreamResponse{
			Status: backend.SubscribeStreamStatusOK,
		}, nil
	}

	return &backend.SubscribeStreamResponse{
		Status: backend.SubscribeStreamStatusPermissionDenied,
	}, backend.DownstreamErrorf("stream path not supported: %s", req.Path)
}

func (ds *DataSource) PublishStream(_ context.Context, _ *backend.PublishStreamRequest) (*backend.PublishStreamResponse, error) {
	ds.logger.Debug("PublishStream called")

	// Do not allow publishing at all.
	return &backend.PublishStreamResponse{
		Status: backend.PublishStreamStatusPermissionDenied,
	}, nil
}

func (ds *DataSource) RunStream(ctx context.Context, request *backend.RunStreamRequest, sender *backend.StreamSender) error {
	ds.logger.Debug("New stream call", "path", request.Path)
	tempoDatasource, dsInfoErr := ds.getDSInfo(ctx, request.PluginContext)
	if dsInfoErr != nil {
		return backend.DownstreamErrorf("failed to get datasource information: %w", dsInfoErr)
	}

	// Get incoming and team HTTP headers
	headers, err := stream_utils.GetHeadersFromIncomingContext(ctx, ds.logger)
	if err != nil {
		return err
	}
	request.Headers = headers

	// Add headers to the outgoing context.
	// This is mainly needed for the API server, as in that case the outgoing context is empty and
	// it is the incoming context that contains the metadata (if any)
	// Headers are set, not appended: the merged headers already include any outgoing metadata, so
	// appending would duplicate them.
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.MD{}
	}
	for key, value := range headers {
		md.Set(key, value)
	}
	ctx = metadata.NewOutgoingContext(ctx, md)

	if strings.HasPrefix(request.Path, SearchPathPrefix) {
		if err = ds.runSearchStream(ctx, request, sender, tempoDatasource); err != nil {
			return sendError(err, sender)
		} else {
			return nil
		}
	}
	if strings.HasPrefix(request.Path, MetricsPathPrefix) {
		if err = ds.runMetricsStream(ctx, request, sender, tempoDatasource); err != nil {
			return sendError(err, sender)
		} else {
			return nil
		}
	}

	return fmt.Errorf("unknown path %s", request.Path)
}
