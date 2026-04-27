package tempo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/traceql"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/tracing"
	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"
	"github.com/grafana/tempo/pkg/tempopb"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const MetricsPathPrefix = "metrics/"

type PartialTempoQuery struct {
	MetricsQueryType *dataquery.MetricsQueryType
}

func (ds *DataSource) runMetricsStream(ctx context.Context, req *backend.RunStreamRequest, sender *backend.StreamSender, datasource *DatasourceInfo) error {
	ctx, span := tracing.DefaultTracer().Start(ctx, "datasource.tempo.runMetricsStream")
	defer span.End()

	response := &backend.DataResponse{}

	var backendQuery *backend.DataQuery
	err := json.Unmarshal(req.Data, &backendQuery)
	if err != nil {
		response.Error = fmt.Errorf("error unmarshaling backend query model: %v", err)
		span.RecordError(response.Error)
		span.SetStatus(codes.Error, response.Error.Error())
		return backend.DownstreamErrorf("error unmarshaling backend query model: %v", err)
	}

	tempoQuery := &PartialTempoQuery{}
	err = json.Unmarshal(req.Data, tempoQuery)
	if err != nil {
		response.Error = fmt.Errorf("error unmarshaling Tempo query model: %v", err)
		span.RecordError(response.Error)
		span.SetStatus(codes.Error, response.Error.Error())
		return backend.DownstreamErrorf("failed to unmarshall Tempo query model: %w", err)
	}

	var qrr *tempopb.QueryRangeRequest
	err = json.Unmarshal(req.Data, &qrr)
	if err != nil {
		response.Error = fmt.Errorf("error unmarshaling Tempo query model: %v", err)
		span.RecordError(response.Error)
		span.SetStatus(codes.Error, response.Error.Error())
		return backend.DownstreamErrorf("failed to unmarshall Tempo query model: %w", err)
	}

	if qrr.GetQuery() == "" {
		return backend.DownstreamErrorf("tempo search query cannot be empty")
	}

	qrr.Start = uint64(backendQuery.TimeRange.From.UnixNano())
	qrr.End = uint64(backendQuery.TimeRange.To.UnixNano())

	if isInstantQuery(tempoQuery.MetricsQueryType) {
		instantQuery := &tempopb.QueryInstantRequest{
			Query: qrr.Query,
			Start: qrr.Start,
			End:   qrr.End,
		}

		stream, err := datasource.StreamingClient.MetricsQueryInstant(ctx, instantQuery)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			ds.logger.Error("Error Search()", "err", err)
			if backend.IsDownstreamHTTPError(err) {
				return backend.DownstreamError(err)
			}
			return err
		}

		return ds.processInstantMetricsStream(ctx, stream, sender)
	}

	stream, err := datasource.StreamingClient.MetricsQueryRange(ctx, qrr)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		ds.logger.Error("Error Search()", "err", err)
		if backend.IsDownstreamHTTPError(err) {
			return backend.DownstreamError(err)
		}
		return err
	}

	return ds.processMetricsStream(ctx, qrr.Query, stream, sender)
}

func (ds *DataSource) processMetricsStream(ctx context.Context, query string, stream tempopb.StreamingQuerier_MetricsQueryRangeClient, sender StreamSender) error {
	ctx, span := tracing.DefaultTracer().Start(ctx, "datasource.tempo.processStream")
	defer span.End()
	messageCount := 0
	for {
		msg, err := stream.Recv()
		messageCount++
		span.SetAttributes(attribute.Int("message_count", messageCount))
		if errors.Is(err, io.EOF) {
			if err := ds.sendResponse(ctx, nil, nil, dataquery.SearchStreamingStateDone, sender); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}
			break
		}
		if err != nil {
			ds.logger.Error("Error receiving message", "err", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		transformed := traceql.TransformMetricsResponse(query, *msg)

		if err := ds.sendResponse(ctx, transformed, msg.Metrics, dataquery.SearchStreamingStateStreaming, sender); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}

	return nil
}

func (ds *DataSource) processInstantMetricsStream(ctx context.Context, stream tempopb.StreamingQuerier_MetricsQueryInstantClient, sender StreamSender) error {
	ctx, span := tracing.DefaultTracer().Start(ctx, "datasource.tempo.processStream")
	defer span.End()
	messageCount := 0
	for {
		msg, err := stream.Recv()
		messageCount++
		span.SetAttributes(attribute.Int("message_count", messageCount))
		if errors.Is(err, io.EOF) {
			if err := ds.sendResponse(ctx, nil, nil, dataquery.SearchStreamingStateDone, sender); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}
			break
		}
		if err != nil {
			ds.logger.Error("Error receiving message", "err", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		transformed := traceql.TransformInstantMetricsResponse(*msg)

		if err := ds.sendResponse(ctx, transformed, msg.Metrics, dataquery.SearchStreamingStateStreaming, sender); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}

	return nil
}
