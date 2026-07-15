package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/tracing"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/tempo/pkg/tempopb"

	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var traceIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]+$`)

func (ds *DataSource) getTrace(ctx context.Context, pCtx backend.PluginContext, query backend.DataQuery) (*backend.DataResponse, error) {
	ctxLogger := ds.logger.FromContext(ctx)
	ctxLogger.Debug("Getting trace", "function", logEntrypoint())

	result := &backend.DataResponse{}
	refID := query.RefID

	ctx, span := tracing.DefaultTracer().Start(ctx, "datasource.tempo.getTrace", trace.WithAttributes(
		attribute.String("queryType", query.QueryType),
	))
	defer span.End()

	model := &dataquery.TempoQuery{}
	err := json.Unmarshal(query.JSON, model)
	if err != nil {
		ctxLogger.Error("Failed to unmarshall Tempo query model", "error", err, "function", logEntrypoint())
		return result, backend.DownstreamErrorf("failed to unmarshall Tempo query model: %w", err)
	}

	dsInfo, err := ds.getDSInfo(ctx, pCtx)
	if err != nil {
		ctxLogger.Error("Failed to get datasource information", "error", err, "function", logEntrypoint())
		return nil, backend.DownstreamErrorf("failed to get datasource information: %w", err)
	}

	if model.Query == nil || *model.Query == "" {
		err := fmt.Errorf("trace id is required")
		ctxLogger.Error("Failed to validate model query", "error", err, "function", logEntrypoint())
		return result, backend.DownstreamErrorf("failed to validate model query: %w", err)
	}

	var apiVersion = TraceRequestApiVersionV2
	//nolint:bodyclose
	resp, traceBody, err := ds.performTraceRequest(ctx, dsInfo, apiVersion, model, query, span)
	if err != nil {
		return result, err
	}

	// If the endpoint is not found, try the v1 endpoint, we might be communicating with an older Tempo version
	if resp.StatusCode == http.StatusNotFound {
		apiVersion = TraceRequestApiVersionV1
		//nolint:bodyclose
		resp, traceBody, err = ds.performTraceRequest(ctx, dsInfo, apiVersion, model, query, span)
		if err != nil {
			return result, err
		}
	}

	if resp.StatusCode != http.StatusOK {
		ctxLogger.Error("Failed to get trace", "error", err, "function", logEntrypoint())
		err := fmt.Errorf("failed to get trace with id: %s Status: %s Body: %s", *model.Query, resp.Status, describeErrorBody(resp, traceBody))

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		if backend.ErrorSourceFromHTTPStatus(resp.StatusCode) == backend.ErrorSourceDownstream {
			return nil, backend.DownstreamError(err)
		}

		return nil, err
	}

	var frame *data.Frame

	if apiVersion == TraceRequestApiVersionV1 {
		var otTrace tempopb.Trace
		err = proto.Unmarshal(traceBody, &otTrace)

		if err != nil {
			ctxLogger.Error("Failed to convert tempo response to Otlp", "error", err, "function", logEntrypoint())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return &backend.DataResponse{}, fmt.Errorf("failed to convert tempo response to Otlp: %w", err)
		}

		frame, err = TraceToFrame(otTrace.GetResourceSpans())
		if err != nil {
			ctxLogger.Error("Failed to transform trace to data frame", "error", err, "function", logEntrypoint())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return &backend.DataResponse{}, fmt.Errorf("failed to transform trace %v to data frame: %w", model.Query, err)
		}

		if frame == nil {
			result.Status = http.StatusNotFound
			err := traceNotFoundError(*model.Query, query.TimeRange)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, backend.DownstreamError(err)
		}
	} else {
		var tr tempopb.TraceByIDResponse
		err = proto.Unmarshal(traceBody, &tr)

		if err != nil {
			ctxLogger.Error("Failed to convert tempo response to Otlp", "error", err, "function", logEntrypoint())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return &backend.DataResponse{}, fmt.Errorf("failed to convert tempo response to Otlp: %w", err)
		}

		frame, err = TraceToFrame(tr.Trace.ResourceSpans)
		if err != nil {
			ctxLogger.Error("Failed to transform trace to data frame", "error", err, "function", logEntrypoint())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return &backend.DataResponse{}, fmt.Errorf("failed to transform trace %v to data frame: %w", model.Query, err)
		}

		if frame == nil {
			result.Status = http.StatusNotFound
			err := traceNotFoundError(*model.Query, query.TimeRange)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, backend.DownstreamError(err)
		}

		frame.Meta.Custom = map[string]interface{}{
			"partial": tr.GetStatus() == tempopb.PartialStatus_PARTIAL,
			"message": tr.GetMessage(),
		}
	}

	frame.RefID = refID
	frames := []*data.Frame{frame}
	result.Frames = frames
	ctxLogger.Debug("Successfully got trace", "function", logEntrypoint())
	return result, nil
}

// traceNotFoundError builds the error returned when Tempo responds successfully
// but the trace has no spans. It includes the searched time range and notes the
// trace may exist outside of it, which is helpful when a time shift is applied
// to the trace-by-id request (issue #176).
func traceNotFoundError(traceID string, timeRange backend.TimeRange) error {
	return fmt.Errorf(
		"trace with id %s not found in the selected time range [%s to %s]; it may exist outside this range",
		traceID,
		timeRange.From.Format(time.RFC3339),
		timeRange.To.Format(time.RFC3339),
	)
}

// isHTMLResponse reports whether a response body is an HTML document rather than
// a Tempo API error. This happens when the Tempo instance is unavailable or a
// proxy/gateway returns an error page instead of a JSON API error.
func isHTMLResponse(resp *http.Response, body []byte) bool {
	if resp != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return true
	}
	trimmed := strings.ToLower(strings.TrimSpace(string(body)))
	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html")
}

// describeErrorBody returns a description of a non-2xx Tempo response body for
// use in error messages. Raw HTML pages are replaced with a user-friendly hint
// instead of being dumped into the UI; JSON/plain error details from Tempo are
// preserved so the actual error reason still reaches the user.
func describeErrorBody(resp *http.Response, body []byte) string {
	if isHTMLResponse(resp, body) {
		return "the Tempo instance may be unavailable or a proxy/gateway returned an HTML error page"
	}
	return string(body)
}

func (ds *DataSource) performTraceRequest(ctx context.Context, dsInfo *DatasourceInfo, apiVersion TraceRequestApiVersion, model *dataquery.TempoQuery, query backend.DataQuery, span trace.Span) (*http.Response, []byte, error) {
	ctxLogger := ds.logger.FromContext(ctx)
	request, err := ds.createRequest(ctx, dsInfo, apiVersion, *model.Query, query.TimeRange.From.Unix(), query.TimeRange.To.Unix())

	if err != nil {
		ctxLogger.Error("Failed to create request", "error", err, "function", logEntrypoint())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, nil, backend.DownstreamErrorf("failed to create request: %w", err)
	}

	resp, err := dsInfo.HTTPClient.Do(request)
	if err != nil {
		ctxLogger.Error("Failed to send request to Tempo", "error", err, "function", logEntrypoint())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if backend.IsDownstreamHTTPError(err) {
			return nil, nil, backend.DownstreamError(err)
		}
		return nil, nil, fmt.Errorf("failed get to tempo: %w", err)
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			if err := resp.Body.Close(); err != nil {
				ctxLogger.Error("Failed to close response body", "error", err, "function", logEntrypoint())
			}
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ctxLogger.Error("Failed to read response body", "error", err, "function", logEntrypoint())
		return nil, nil, err
	}
	return resp, body, nil
}

type TraceRequestApiVersion int

const (
	TraceRequestApiVersionV1 TraceRequestApiVersion = iota
	TraceRequestApiVersionV2
)

func (ds *DataSource) createRequest(ctx context.Context, dsInfo *DatasourceInfo, apiVersion TraceRequestApiVersion, traceID string, start int64, end int64) (*http.Request, error) {
	ctxLogger := ds.logger.FromContext(ctx)
	var baseUrl string
	var tempoQuery string

	if !traceIDPattern.MatchString(traceID) {
		return nil, backend.DownstreamErrorf("invalid trace id")
	}

	if apiVersion == TraceRequestApiVersionV1 {
		baseUrl = fmt.Sprintf("%s/api/traces/%s", dsInfo.URL, traceID)
	} else {
		baseUrl = fmt.Sprintf("%s/api/v2/traces/%s", dsInfo.URL, traceID)
	}

	if start == 0 || end == 0 {
		tempoQuery = baseUrl
	} else {
		tempoQuery = fmt.Sprintf("%s?start=%d&end=%d", baseUrl, start, end)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", tempoQuery, nil)
	if err != nil {
		ctxLogger.Error("Failed to create request", "error", err, "function", logEntrypoint())
		return nil, err
	}

	req.Header.Set("Accept", "application/protobuf")
	return req, nil
}
