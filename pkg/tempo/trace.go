package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
// but the trace has no spans. When a real time range is set it is included, with
// a note that the trace may exist outside of it, which is helpful when a time
// shift is applied to the trace-by-id request (issue #176).
//
// A zero bound means no range was applied: createRequest omits start/end when
// either is zero (the frontend zeroes the range when time shift is off, which is
// the default), so Tempo searches all time. Reporting a [1970-.. to 1970-..]
// range in that case is misleading, so fall back to a plain message.
func traceNotFoundError(traceID string, timeRange backend.TimeRange) error {
	if timeRange.From.Unix() == 0 || timeRange.To.Unix() == 0 {
		return fmt.Errorf("trace with id %s not found", traceID)
	}
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
	request, err := ds.createRequest(ctx, dsInfo, apiVersion, model, *model.Query, query.TimeRange.From.Unix(), query.TimeRange.To.Unix())

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

func (ds *DataSource) createRequest(ctx context.Context, dsInfo *DatasourceInfo, apiVersion TraceRequestApiVersion, model *dataquery.TempoQuery, traceID string, start int64, end int64) (*http.Request, error) {
	ctxLogger := ds.logger.FromContext(ctx)

	if !traceIDPattern.MatchString(traceID) {
		return nil, backend.DownstreamErrorf("invalid trace id")
	}

	baseUrl, err := url.Parse(dsInfo.URL)
	if err != nil {
		ctxLogger.Error("Failed to parse trace URL", "url", dsInfo.URL, "error", err, "function", logEntrypoint())
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	var traceUrl *url.URL
	if apiVersion == TraceRequestApiVersionV1 {
		traceUrl = baseUrl.JoinPath("api", "traces", traceID)
	} else {
		traceUrl = baseUrl.JoinPath("api", "v2", "traces", traceID)
	}

	// Using url.Values keeps any query parameters already present in the configured
	// data source URL instead of clobbering them with a second "?". The read and the
	// write-back stay outside the time range guard: the frontend zeroes the range
	// when time shift is off (the default), so params other than start/end would
	// otherwise never be encoded in the common case.
	q := traceUrl.Query()
	if start != 0 && end != 0 {
		q.Set("start", strconv.FormatInt(start, 10))
		q.Set("end", strconv.FormatInt(end, 10))
	}
	// The v1 endpoint does not support span pruning params, and getTrace falls back
	// to v1 on a 404.
	if apiVersion == TraceRequestApiVersionV2 {
		appendSpanPruningParams(q, model)
	}
	traceUrl.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", traceUrl.String(), nil)
	if err != nil {
		ctxLogger.Error("Failed to create request", "error", err, "function", logEntrypoint())
		return nil, err
	}

	req.Header.Set("Accept", "application/protobuf")
	return req, nil
}

// appendSpanPruningParams adds the span pruning query params for the v2
// trace-by-id endpoint. span_pruning is always sent so the request does not
// depend on the cluster or tenant default.
//
// The true default here must stay paired with `query.spanPruning ?? true` in
// src/traceql/TraceIdQueryOptions.tsx; there is no shared source of truth across
// the TS/Go boundary.
func appendSpanPruningParams(q url.Values, model *dataquery.TempoQuery) {
	pruning := true
	if model.SpanPruning != nil {
		pruning = *model.SpanPruning
	}
	q.Set("span_pruning", strconv.FormatBool(pruning))
	if !pruning {
		// Tempo only reads the sub-params when pruning is enabled.
		return
	}

	if model.SpanPruningGroupBy != nil && *model.SpanPruningGroupBy != "" {
		q.Set("span_pruning_group_by", *model.SpanPruningGroupBy)
	}
	if model.SpanPruningMinSpans != nil {
		q.Set("span_pruning_min_spans", strconv.FormatInt(*model.SpanPruningMinSpans, 10))
	}
	// No positivity guard: 0 (aggregate leaves only) and -1 (unlimited) are both
	// meaningful values.
	if model.SpanPruningMaxParentDepth != nil {
		q.Set("span_pruning_max_parent_depth", strconv.FormatInt(*model.SpanPruningMaxParentDepth, 10))
	}
}
