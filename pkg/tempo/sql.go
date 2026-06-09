package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"
	schemas "github.com/grafana/schemads"
)

// schemadsQuery extends schemas.Query with aggregation pushdown for Grafana SQL metrics.
type schemadsQuery struct {
	schemas.Query
	Aggregation *aggregationHint `json:"aggregation,omitempty"`
}

type aggregationHint struct {
	Function string   `json:"function"` // COUNT, AVG, SUM, MIN, MAX
	Column   string   `json:"column"`
	GroupBy  []string `json:"groupBy"`
}

// Fixed context strings for scalarToString errors: identify the conversion step without echoing
// user query text, tag names, or other payload content (PII-safe for logs and API errors).
const (
	scalarConvCtxLikePattern       = "converting LIKE pattern"
	scalarConvCtxMultiValueOperand = "converting multi-value match operand"
)

// normalizeGrafanaSQLRequest translates dsabstraction Grafana SQL payloads into Tempo queries.
//
// Normalized span-table queries use queryType "traceql" (not "traceqlSearch"): runTraceQlQuery routes
// metrics vs search via isMetricsQuery(query), not via queryType. tableType "spans" is still required
// so Search() selects span frames instead of defaulting to traces.
// sqlErrors maps refId to validation or conversion errors for queries that were not converted.
// metricsRefIDs lists refIDs converted to TraceQL metrics queries (flatten responses for these).
func (ds *DataSource) normalizeGrafanaSQLRequest(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataRequest, map[string]error, map[string]struct{}) {
	_ = ctx
	if req == nil || len(req.Queries) == 0 {
		return req, nil, nil
	}

	grafanaConfig := req.PluginContext.GrafanaConfig
	if grafanaConfig == nil || !grafanaConfig.FeatureToggles().IsEnabled("dsAbstractionApp") {
		return req, nil, nil
	}

	out := make([]backend.DataQuery, 0, len(req.Queries))
	sqlErrors := make(map[string]error)
	metricsRefIDs := make(map[string]struct{})

	for _, q := range req.Queries {
		var sq schemadsQuery
		if err := json.Unmarshal(q.JSON, &sq); err != nil {
			out = append(out, q)
			continue
		}
		if !sq.GrafanaSql {
			out = append(out, q)
			continue
		}

		table := strings.TrimSpace(sq.Table)
		if table == "" {
			sqlErrors[q.RefID] = fmt.Errorf("tempo grafana sql: table is required when grafanaSql is true")
			continue
		}

		model := dataquery.NewTempoQuery()
		model.RefId = q.RefID
		qt := string(dataquery.TempoQueryTypeTraceql)
		model.QueryType = &qt
		tt := dataquery.SearchTableTypeSpans
		model.TableType = &tt

		var traceQL string
		var err error
		// Table selects execution path; dsabstraction must emit span_metrics for SQL with FOR (...).
		switch table {
		case tempoSchemadsTableSpans:
			if sq.Aggregation != nil {
				sqlErrors[q.RefID] = fmt.Errorf(
					"tempo grafana sql: aggregation on %q is not supported; use %q for TraceQL metrics",
					tempoSchemadsTableSpans, tempoSchemadsTableSpanMetrics,
				)
				continue
			}
			traceQL, err = traceQLFromSchemadsFilters(sq.Filters)
			if err != nil {
				sqlErrors[q.RefID] = err
				continue
			}
			if sq.Limit != nil && *sq.Limit > 0 {
				model.Limit = sq.Limit
			}
		case tempoSchemadsTableSpanMetrics:
			if sq.Aggregation == nil {
				sqlErrors[q.RefID] = fmt.Errorf(
					"tempo grafana sql: %q requires aggregation for TraceQL metrics",
					tempoSchemadsTableSpanMetrics,
				)
				continue
			}
			traceQL, err = buildTraceQLMetricsQuery(sq)
			if err != nil {
				sqlErrors[q.RefID] = err
				continue
			}
			if err := applyMetricsTableHints(model, sq.TableHintValues); err != nil {
				sqlErrors[q.RefID] = err
				continue
			}
			metricsRefIDs[q.RefID] = struct{}{}
		default:
			sqlErrors[q.RefID] = fmt.Errorf("tempo grafana sql: %s", unsupportedSchemadsTableError(table))
			continue
		}
		model.Query = &traceQL

		raw, err := json.Marshal(model)
		if err != nil {
			sqlErrors[q.RefID] = err
			continue
		}

		out = append(out, backend.DataQuery{
			RefID:         q.RefID,
			QueryType:     string(dataquery.TempoQueryTypeTraceql),
			TimeRange:     q.TimeRange,
			Interval:      q.Interval,
			MaxDataPoints: q.MaxDataPoints,
			JSON:          raw,
		})
	}

	if len(sqlErrors) == 0 {
		sqlErrors = nil
	}
	if len(metricsRefIDs) == 0 {
		metricsRefIDs = nil
	}
	return &backend.QueryDataRequest{
		PluginContext: req.PluginContext,
		Headers:       req.Headers,
		Queries:       out,
	}, sqlErrors, metricsRefIDs
}

func buildTraceQLMetricsQuery(sq schemadsQuery) (string, error) {
	selector, err := traceQLFromSchemadsFilters(sq.Filters)
	if err != nil {
		return "", err
	}

	var fn string
	if _, ok := sq.TableHintValues["RATE"]; ok {
		fn = "| rate()"
	} else if sq.Aggregation == nil {
		return "", fmt.Errorf("tempo grafana sql: aggregation is required for metrics queries")
	} else {
		fn, err = traceQLMetricsFunction(sq.Aggregation)
		if err != nil {
			return "", err
		}
	}

	groupBy, err := traceQLMetricsGroupBy(sq.Aggregation)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(selector + " " + fn + groupBy), nil
}

func traceQLMetricsFunction(agg *aggregationHint) (string, error) {
	if agg == nil {
		return "", fmt.Errorf("tempo grafana sql: aggregation is required for metrics queries")
	}
	fn := strings.ToUpper(strings.TrimSpace(agg.Function))
	switch fn {
	case "COUNT":
		return "| count_over_time()", nil
	case "AVG", "SUM", "MIN", "MAX":
		attr, err := metricsOverTimeAttribute(agg.Column)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("| %s_over_time(%s)", strings.ToLower(fn), attr), nil
	default:
		return "", fmt.Errorf("tempo grafana sql: unsupported aggregation function %q", agg.Function)
	}
}

func metricsOverTimeAttribute(column string) (string, error) {
	col := strings.TrimSpace(column)
	if col == "" || col == "duration" || col == "value" {
		return "span:duration", nil
	}
	return traceqlSelectorFromSpansColumn(col)
}

func traceQLMetricsGroupBy(agg *aggregationHint) (string, error) {
	if agg == nil || len(agg.GroupBy) == 0 {
		return "", nil
	}
	var labels []string
	for _, col := range agg.GroupBy {
		// SQL result columns (time/timestamp/value) are not TraceQL group-by attributes.
		switch col {
		case "timestamp", "value", "time":
			continue
		}
		sel, err := traceqlSelectorFromSpansColumn(col)
		if err != nil {
			return "", err
		}
		labels = append(labels, sel)
	}
	if len(labels) == 0 {
		return "", nil
	}
	return " by (" + strings.Join(labels, ", ") + ")", nil
}

func applyMetricsTableHints(model *dataquery.TempoQuery, hints map[string]string) error {
	if hints == nil {
		return nil
	}
	if _, ok := hints["INSTANT"]; ok {
		instant := dataquery.MetricsQueryTypeInstant
		model.MetricsQueryType = &instant
	}
	if step, ok := hints["STEP"]; ok {
		model.Step = &step
	}
	if ex, ok := hints["EXEMPLARS"]; ok {
		n, err := strconv.ParseInt(strings.TrimSpace(ex), 10, 64)
		if err != nil {
			return fmt.Errorf("tempo grafana sql: invalid EXEMPLARS hint value %q", ex)
		}
		model.Exemplars = &n
	}
	return nil
}

// traceQLFromSchemadsFilters builds a TraceQL span-search selector `{...}` from schemads column filters.
func traceQLFromSchemadsFilters(filters []schemas.ColumnFilter) (string, error) {
	var parts []string
	for _, cf := range filters {
		if cf.Name == "" || len(cf.Conditions) == 0 {
			continue
		}
		sel, err := traceqlSelectorFromSpansColumn(cf.Name)
		if err != nil {
			return "", err
		}
		for _, cond := range cf.Conditions {
			clauses, err := filterConditionToTraceQL(sel, cond)
			if err != nil {
				return "", err
			}
			parts = append(parts, clauses...)
		}
	}
	if len(parts) == 0 {
		return "{}", nil
	}
	return "{" + strings.Join(parts, " && ") + "}", nil
}

// traceQLEnumIntrinsics are TraceQL attributes whose enum operands must not be quoted.
// Only kind/status (and scoped span:kind/span:status) use enum literals; all other
// intrinsics use quoted strings, numbers, or durations per Tempo docs.
// See grafana/tempo/pkg/traceql/lexer.go tokenMap and construct-traceql-queries intrinsic table.
var traceQLEnumIntrinsics = map[string]struct{}{
	"kind":        {},
	"span:kind":   {},
	"status":      {},
	"span:status": {},
}

// traceQLStatusEnumValues matches Tempo lexer keywords STATUS_OK, STATUS_ERROR, STATUS_UNSET.
var traceQLStatusEnumValues = map[string]struct{}{
	"ok": {}, "unset": {}, "error": {},
}

// traceQLKindEnumValues matches Tempo lexer keywords KIND_*.
var traceQLKindEnumValues = map[string]struct{}{
	"unspecified": {}, "internal": {}, "server": {}, "client": {}, "producer": {}, "consumer": {},
}

func isTraceQLEnumLiteral(selector, value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	switch selector {
	case "status", "span:status":
		_, ok := traceQLStatusEnumValues[v]
		return ok
	case "kind", "span:kind":
		_, ok := traceQLKindEnumValues[v]
		return ok
	default:
		return false
	}
}

func traceQLEnumLiteralOperand(selector, value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// bareTraceQLSearchIntrinsics are span-search attributes that use no scope prefix and no leading dot.
var bareTraceQLSearchIntrinsics = map[string]struct{}{
	"duration":           {},
	"kind":               {},
	"name":               {},
	"rootName":           {},
	"rootServiceName":    {},
	"status":             {},
	"statusMessage":      {},
	"traceDuration":      {},
	"trace:id":           {},
	"span:id":            {},
	"span:name":          {},
	"span:duration":      {},
	"span:kind":          {},
	"span:status":        {},
	"span:statusMessage": {},
}

func traceqlSelectorFromSpansColumn(col string) (string, error) {
	switch col {
	case tempoSpanColTraceIDHidden:
		return "trace:id", nil
	case tempoSpanColTraceService:
		return "rootServiceName", nil
	case tempoSpanColTraceName:
		return "rootName", nil
	case tempoSpanColSpanID:
		return "span:id", nil
	case tempoSpanColTime:
		return "", fmt.Errorf("tempo grafana sql: filtering on column %q is not supported; use the query time range", col)
	case tempoSpanColTimestamp:
		return "", fmt.Errorf("tempo grafana sql: filtering on column %q is not supported; metrics use the query time range", col)
	case tempoSpanColValue:
		return "", fmt.Errorf("tempo grafana sql: column %q is a SQL metric result column, not a TraceQL filter", col)
	case tempoSpanColName:
		return "name", nil
	case tempoSpanColDuration:
		return "duration", nil
	default:
		if strings.HasPrefix(col, "resource.") || strings.HasPrefix(col, "span.") ||
			strings.HasPrefix(col, "event.") || strings.HasPrefix(col, "instrumentation.") ||
			strings.HasPrefix(col, "link.") {
			return col, nil
		}
		if _, ok := bareTraceQLSearchIntrinsics[col]; ok {
			return col, nil
		}
		// Unscoped / unknown tag: leading-dot form matches TempoLanguageProvider for non-intrinsic tags.
		return "." + col, nil
	}
}

func filterConditionToTraceQL(selector string, fc schemas.FilterCondition) ([]string, error) {
	op := fc.Operator
	switch op {
	case schemas.OperatorEquals, schemas.OperatorNotEquals, schemas.OperatorGreaterThan, schemas.OperatorGreaterThanOrEqual,
		schemas.OperatorLessThan, schemas.OperatorLessThanOrEqual, schemas.OperatorLike, schemas.OperatorIn:
	default:
		return nil, fmt.Errorf("tempo grafana sql: unsupported operator %q", op)
	}

	if op == schemas.OperatorIn {
		if len(fc.Values) == 0 {
			return nil, fmt.Errorf("tempo grafana sql: %q operator requires values", op)
		}
		return []string{inConditionToTraceQL(selector, fc.Values)}, nil
	}

	if len(fc.Values) > 0 && op != schemas.OperatorLike {
		if len(fc.Values) == 1 {
			return []string{selector + string(op) + formatTraceQLOperand(selector, fc.Values[0], true)}, nil
		}
		return multiValueNonLikeToTraceQL(selector, op, fc.Values)
	}

	if fc.Value == nil {
		return nil, fmt.Errorf("tempo grafana sql: missing value for condition on %q", selector)
	}

	if op == schemas.OperatorLike {
		s, err := scalarToString(fc.Value, scalarConvCtxLikePattern)
		if err != nil {
			return nil, err
		}
		re := likePatternToRegex(s)
		return []string{fmt.Sprintf("%s=~%q", selector, re)}, nil
	}

	return []string{selector + string(op) + formatTraceQLOperand(selector, fc.Value, true)}, nil
}

func inConditionToTraceQL(selector string, values []any) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, selector+"="+formatTraceQLOperand(selector, v, true))
	}
	return "(" + strings.Join(parts, " || ") + ")"
}

func multiValueNonLikeToTraceQL(selector string, op schemas.Operator, values []any) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("tempo grafana sql: empty values")
	}
	if op == schemas.OperatorEquals {
		pipe, err := joinPipeQuotedValues(values)
		if err != nil {
			return nil, err
		}
		return []string{selector + "=" + pipe}, nil
	}
	if op == schemas.OperatorNotEquals {
		out := make([]string, 0, len(values))
		for _, v := range values {
			out = append(out, selector+string(op)+formatTraceQLOperand(selector, v, true))
		}
		return out, nil
	}
	return nil, fmt.Errorf("tempo grafana sql: multiple values not supported for operator %s", op)
}

func joinPipeQuotedValues(values []any) (string, error) {
	ss := make([]string, 0, len(values))
	for _, v := range values {
		s, err := scalarToString(v, scalarConvCtxMultiValueOperand)
		if err != nil {
			return "", err
		}
		ss = append(ss, s)
	}
	return `"` + strings.Join(ss, "|") + `"`, nil
}

func formatTraceQLOperand(selector string, v any, inferString bool) string {
	switch t := v.(type) {
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case string:
		if inferString && !looksNumeric(t) {
			if _, isEnum := traceQLEnumIntrinsics[selector]; isEnum && isTraceQLEnumLiteral(selector, t) {
				return traceQLEnumLiteralOperand(selector, t)
			}
			return strconv.Quote(t)
		}
		return t
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if inferString && !looksNumeric(s) {
			if _, isEnum := traceQLEnumIntrinsics[selector]; isEnum && isTraceQLEnumLiteral(selector, s) {
				return traceQLEnumLiteralOperand(selector, s)
			}
			return strconv.Quote(s)
		}
		return s
	}
}

func looksNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	return false
}

func scalarToString(v any, convCtx string) (string, error) {
	if v == nil {
		return "", fmt.Errorf("tempo grafana sql: %s: value is null (expected string, number, or boolean)", convCtx)
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case json.Number:
		return t.String(), nil
	case bool:
		return strconv.FormatBool(t), nil
	default:
		return "", fmt.Errorf("tempo grafana sql: %s: unsupported value type %T (expected string, number, or boolean)", convCtx, v)
	}
}

func likePatternToRegex(like string) string {
	// Minimal SQL LIKE → regexp: % -> .*, _ -> ., escape regex metacharacters in literals.
	// Anchor with ^...$ so the match follows SQL LIKE full-string semantics (not substring).
	var b strings.Builder
	runes := []rune(like)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteString(".")
		case '\\':
			if i+1 < len(runes) {
				i++
				b.WriteString(regexp.QuoteMeta(string(runes[i])))
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		}
	}
	return "^" + b.String() + "$"
}
