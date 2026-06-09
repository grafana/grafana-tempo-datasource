package tempo

import (
	"context"
	"fmt"
	"sort"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"
	schemas "github.com/grafana/schemads"
)

// Grafana SQL schemads tables (see sql.go for query routing):
//   spans        — TraceQL span search rows; output column "time"
//   span_metrics — TraceQL metrics time series; requires aggregation; output "timestamp"/"value"
const (
	tempoSchemadsTableSpans       = "spans"
	tempoSchemadsTableSpanMetrics = "span_metrics"

	tempoSpanColTraceIDHidden = "traceIdHidden"
	tempoSpanColTraceService  = "traceService"
	tempoSpanColTraceName     = "traceName"
	tempoSpanColSpanID        = "spanID"
	tempoSpanColTime          = "time"
	tempoSpanColName          = "name"
	tempoSpanColDuration      = "duration"
	tempoSpanColTimestamp     = "timestamp"
	tempoSpanColValue         = "value"
)

func tempoSchemadsTables() []string {
	return []string{tempoSchemadsTableSpans, tempoSchemadsTableSpanMetrics}
}

// traceqlStringColumnOperators matches TraceQL attribute filters (=, !=, in, like/regex).
// See https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/
func traceqlStringColumnOperators() []schemas.Operator {
	return []schemas.Operator{
		schemas.OperatorEquals,
		schemas.OperatorNotEquals,
		schemas.OperatorIn,
		schemas.OperatorLike,
	}
}

// traceqlIdentifierColumnOperators is for trace:id and span:id (exact / multi-value match).
func traceqlIdentifierColumnOperators() []schemas.Operator {
	return []schemas.Operator{
		schemas.OperatorEquals,
		schemas.OperatorNotEquals,
		schemas.OperatorIn,
	}
}

// traceqlDurationColumnOperators matches TraceQL duration intrinsics (span:duration, duration).
func traceqlDurationColumnOperators() []schemas.Operator {
	return []schemas.Operator{
		schemas.OperatorEquals,
		schemas.OperatorNotEquals,
		schemas.OperatorGreaterThan,
		schemas.OperatorGreaterThanOrEqual,
		schemas.OperatorLessThan,
		schemas.OperatorLessThanOrEqual,
	}
}

// tempoSchemaProvider implements schemads resource handlers for the Tempo datasource (schema metadata for dsabstraction).
type tempoSchemaProvider struct {
	ds     *DataSource
	logger log.Logger
}

func newTempoSchemaProvider(ds *DataSource, logger log.Logger) *tempoSchemaProvider {
	return &tempoSchemaProvider{ds: ds, logger: logger}
}

func (p *tempoSchemaProvider) dsInfo(ctx context.Context) (*DatasourceInfo, error) {
	pc := backend.PluginConfigFromContext(ctx)
	return p.ds.getDSInfo(ctx, pc)
}

func schemaBoolPtr(b bool) *bool {
	return &b
}

// spanFilterColumns returns WHERE / GROUP BY columns shared by spans and span_metrics.
func spanFilterColumns() []schemas.Column {
	falsePtr := schemaBoolPtr(false)
	truePtr := schemaBoolPtr(true)
	traceqlStringOps := traceqlStringColumnOperators()
	traceqlIDOps := traceqlIdentifierColumnOperators()
	traceqlDurOps := traceqlDurationColumnOperators()
	return []schemas.Column{
		{
			Name: tempoSpanColTraceIDHidden, Type: schemas.ColumnTypeString, Operators: traceqlIDOps,
			Metadata: schemas.Metadata{Description: "Trace ID"}, SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColTraceService, Type: schemas.ColumnTypeString, Operators: traceqlStringOps,
			Metadata: schemas.Metadata{Description: "Root trace service"}, SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColTraceName, Type: schemas.ColumnTypeString, Operators: traceqlStringOps,
			Metadata: schemas.Metadata{Description: "Root trace name"}, SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColSpanID, Type: schemas.ColumnTypeString, Operators: traceqlIDOps,
			Metadata: schemas.Metadata{Description: "Span ID"}, SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColName, Type: schemas.ColumnTypeString, Operators: traceqlStringOps,
			Metadata: schemas.Metadata{Description: "Span name"}, SupportsValues: truePtr,
		},
		{
			Name: tempoSpanColDuration, Type: schemas.ColumnTypeFloat64, Operators: traceqlDurOps,
			Metadata: schemas.Metadata{Description: "Span duration"}, SupportsValues: truePtr,
		},
	}
}

// spansOutputColumns returns span-row output columns (spans table only).
func spansOutputColumns() []schemas.Column {
	falsePtr := schemaBoolPtr(false)
	return []schemas.Column{
		{
			Name: tempoSpanColTime, Type: schemas.ColumnTypeDatetime, Operators: nil,
			Metadata: schemas.Metadata{Description: "Span start time"}, SupportsValues: falsePtr,
		},
	}
}

// spanMetricsResultColumns returns TraceQL metrics output columns (span_metrics table only).
func spanMetricsResultColumns() []schemas.Column {
	falsePtr := schemaBoolPtr(false)
	return []schemas.Column{
		{
			Name: tempoSpanColTimestamp, Type: schemas.ColumnTypeDatetime,
			Metadata: schemas.Metadata{Description: "Metrics step time"}, SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColValue, Type: schemas.ColumnTypeFloat64,
			Metadata: schemas.Metadata{Description: "Metric sample (output only)"}, SupportsValues: falsePtr,
		},
	}
}

// spansFixedColumns returns all fixed columns for the spans table.
func spansFixedColumns() []schemas.Column {
	return append(spanFilterColumns(), spansOutputColumns()...)
}

// spanMetricsFixedColumns returns all fixed columns for the span_metrics table.
func spanMetricsFixedColumns() []schemas.Column {
	return append(spanFilterColumns(), spanMetricsResultColumns()...)
}

// mergeTableColumns merges fixed and dynamic tag columns. Fixed names win over dynamic tags.
func mergeTableColumns(fixed, dynamic []schemas.Column) []schemas.Column {
	seen := make(map[string]struct{}, len(fixed)+len(dynamic))
	out := make([]schemas.Column, 0, len(fixed)+len(dynamic))
	for _, c := range fixed {
		if c.Name == "" {
			continue
		}
		if _, ok := seen[c.Name]; ok {
			continue
		}
		seen[c.Name] = struct{}{}
		out = append(out, c)
	}
	for _, c := range dynamic {
		if c.Name == "" {
			continue
		}
		if _, ok := seen[c.Name]; ok {
			continue
		}
		seen[c.Name] = struct{}{}
		out = append(out, c)
	}
	return out
}

func spansTableColumns(dynamic []schemas.Column) []schemas.Column {
	return mergeTableColumns(spansFixedColumns(), dynamic)
}

func spanMetricsTableColumns(dynamic []schemas.Column) []schemas.Column {
	return mergeTableColumns(spanMetricsFixedColumns(), dynamic)
}

func spansQueryPatterns() []map[string]string {
	return []map[string]string{
		{
			"mode": "span_tabular",
			"sql":  "SELECT time, name, traceService FROM `tempo::<uid>`.spans LIMIT 25",
		},
		{
			"mode": "span_duration",
			"sql":  "SELECT traceService, sum(duration) FROM `tempo::<uid>`.spans GROUP BY traceService LIMIT 15",
		},
	}
}

func spanMetricsQueryPatterns() []map[string]string {
	return []map[string]string{
		{
			"mode": "metrics_count",
			"sql":  "SELECT timestamp, count(value) AS cnt FROM `tempo::<uid>`.span_metrics FOR (step('30s')) GROUP BY timestamp LIMIT 15",
		},
		{
			"mode": "metrics_rate",
			"sql":  "SELECT timestamp, count(value) AS rps FROM `tempo::<uid>`.span_metrics FOR (rate(), step('30s')) GROUP BY timestamp LIMIT 15",
		},
		{
			"mode": "metrics_duration",
			"sql":  "SELECT timestamp, sum(duration) FROM `tempo::<uid>`.span_metrics FOR (step('30s')) GROUP BY timestamp LIMIT 15",
		},
	}
}

func spansTableMetadata() schemas.Metadata {
	return schemas.Metadata{
		Description: "TraceQL span search rows. GROUP BY aggregates run in the Grafana SQL engine (not pushed to Tempo). Use span_metrics for time series.",
		Custom: map[string]any{
			"queryPatterns": spansQueryPatterns(),
		},
	}
}

func spanMetricsTableMetadata() schemas.Metadata {
	return schemas.Metadata{
		Description: "TraceQL metrics time series. Requires aggregation and FOR.",
		Custom: map[string]any{
			"queryPatterns": spanMetricsQueryPatterns(),
			"maxTimeRange":  "3h",
		},
	}
}

func spansTable(dynamic []schemas.Column) schemas.Table {
	return schemas.Table{
		Name:     tempoSchemadsTableSpans,
		Columns:  spansTableColumns(dynamic),
		Metadata: spansTableMetadata(),
	}
}

// spanMetricsTableHints map SQL FOR (...) clauses to TraceQL metrics API options.
func spanMetricsTableHints() []schemas.TableHint {
	return []schemas.TableHint{
		{
			Name:        "rate",
			Description: "TraceQL span rate (| rate()). Use FOR (rate()) with empty parentheses. Unlike Loki, no duration — use step('30s') for resolution.",
			HasValue:    true,
		},
		{Name: "step", Description: "Query step, e.g. step('30s')", HasValue: true},
		{Name: "instant", Description: "Instant metrics query"},
		{Name: "exemplars", Description: "Max exemplars (0 = none)", HasValue: true},
	}
}

func spanMetricsTable(dynamic []schemas.Column) schemas.Table {
	return schemas.Table{
		Name:         tempoSchemadsTableSpanMetrics,
		Columns:      spanMetricsTableColumns(dynamic),
		TableHints:   spanMetricsTableHints(),
		Capabilities: tempoMetricsCapabilities,
		Metadata:     spanMetricsTableMetadata(),
	}
}

// tempoMetricsCapabilities applies to span_metrics only. Aggregates compile via
// aggregation JSON in sql.go (COUNT → count_over_time; AVG/SUM/MIN/MAX → *_over_time).
// rate() uses the rate table hint.
var tempoMetricsCapabilities = &schemas.DatasourceCapabilities{
	AggregateFunctions: []schemas.AggregateFunction{
		schemas.AggregateCount,
		schemas.AggregateAvg,
		schemas.AggregateSum,
		schemas.AggregateMin,
		schemas.AggregateMax,
	},
	Limit: true,
}

// Schema implements schemas.SchemaHandler.
func (p *tempoSchemaProvider) Schema(ctx context.Context, _ *schemas.SchemaRequest) (*schemas.SchemaResponse, error) {
	dsInfo, err := p.dsInfo(ctx)
	if err != nil {
		return &schemas.SchemaResponse{Errors: err.Error()}, nil
	}
	tagCols, tagErr := p.dynamicTagColumns(ctx, dsInfo)
	if tagErr != nil {
		p.logger.Warn("tempo schemads: failed to load tags for schema", "error", tagErr)
		tagCols = nil
	}
	resp := &schemas.SchemaResponse{
		FullSchema: &schemas.Schema{
			Tables: []schemas.Table{
				spansTable(tagCols),
				spanMetricsTable(tagCols),
			},
		},
	}
	if tagErr != nil {
		resp.Errors = fmt.Sprintf("attribute columns unavailable: %v", tagErr)
	}
	return resp, nil
}

// Tables implements schemas.TablesHandler.
func (p *tempoSchemaProvider) Tables(ctx context.Context, _ *schemas.TablesRequest) (*schemas.TablesResponse, error) {
	_, err := p.dsInfo(ctx)
	if err != nil {
		errs := make(map[string]string, len(tempoSchemadsTables()))
		for _, t := range tempoSchemadsTables() {
			errs[t] = err.Error()
		}
		return &schemas.TablesResponse{Errors: errs}, nil
	}
	return &schemas.TablesResponse{
		Tables: tempoSchemadsTables(),
		TableMetadata: map[string]schemas.Metadata{
			tempoSchemadsTableSpans:       spansTableMetadata(),
			tempoSchemadsTableSpanMetrics: spanMetricsTableMetadata(),
		},
		TableCapabilities: map[string]*schemas.DatasourceCapabilities{
			tempoSchemadsTableSpanMetrics: tempoMetricsCapabilities,
		},
	}, nil
}

func isSupportedSchemadsTable(table string) bool {
	return table == tempoSchemadsTableSpans || table == tempoSchemadsTableSpanMetrics
}

func schemadsColumnsForTable(table string, tagCols []schemas.Column) ([]schemas.Column, schemas.Metadata) {
	switch table {
	case tempoSchemadsTableSpans:
		return spansTableColumns(tagCols), spansTableMetadata()
	case tempoSchemadsTableSpanMetrics:
		return spanMetricsTableColumns(tagCols), spanMetricsTableMetadata()
	default:
		return nil, schemas.Metadata{}
	}
}

// Columns implements schemas.ColumnsHandler.
func (p *tempoSchemaProvider) Columns(ctx context.Context, req *schemas.ColumnsRequest) (*schemas.ColumnsResponse, error) {
	out := make(map[string][]schemas.Column, len(req.Tables))
	tableMetadata := make(map[string]schemas.Metadata, len(req.Tables))
	errs := make(map[string]string)

	var supported []string
	for _, table := range req.Tables {
		if isSupportedSchemadsTable(table) {
			supported = append(supported, table)
			continue
		}
		errs[table] = unsupportedSchemadsTableError(table)
	}
	if len(supported) == 0 {
		return &schemas.ColumnsResponse{Columns: out, Errors: errsOrNil(errs)}, nil
	}

	dsInfo, err := p.dsInfo(ctx)
	if err != nil {
		for _, table := range supported {
			errs[table] = err.Error()
		}
		return &schemas.ColumnsResponse{Columns: out, Errors: errsOrNil(errs)}, nil
	}

	tagCols, tagErr := p.dynamicTagColumns(ctx, dsInfo)
	if tagErr != nil {
		p.logger.Warn("tempo schemads: failed to load tags for columns", "error", tagErr)
		msg := fmt.Sprintf("attribute columns unavailable: %v", tagErr)
		for _, table := range supported {
			errs[table] = msg
		}
	}

	for _, table := range supported {
		cols, meta := schemadsColumnsForTable(table, tagCols)
		out[table] = cols
		tableMetadata[table] = meta
	}

	return &schemas.ColumnsResponse{
		Columns:       out,
		TableMetadata: tableMetadata,
		Errors:        errsOrNil(errs),
	}, nil
}

func errsOrNil(errs map[string]string) map[string]string {
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func unsupportedSchemadsTableError(table string) string {
	return fmt.Sprintf("unsupported table %q (supported: %q, %q)", table, tempoSchemadsTableSpans, tempoSchemadsTableSpanMetrics)
}

// ColumnValues implements schemas.ColumnValuesHandler.
func (p *tempoSchemaProvider) ColumnValues(ctx context.Context, req *schemas.ColumnValuesRequest) (*schemas.ColumnValuesResponse, error) {
	out := make(map[string][]string, len(req.Columns))
	for _, c := range req.Columns {
		out[c] = nil
	}
	if len(req.Columns) == 0 {
		return &schemas.ColumnValuesResponse{ColumnValues: out}, nil
	}

	var noTagValues map[string]struct{}
	switch req.Table {
	case tempoSchemadsTableSpans:
		noTagValues = tableFixedColumnNamesNoTagValues(spansFixedColumns())
	case tempoSchemadsTableSpanMetrics:
		noTagValues = tableFixedColumnNamesNoTagValues(spanMetricsFixedColumns())
	default:
		return &schemas.ColumnValuesResponse{
			ColumnValues: out,
			Errors:       globalColumnValuesErrors(req.Columns, nil, unsupportedSchemadsTableError(req.Table)),
		}, nil
	}

	dsInfo, err := p.dsInfo(ctx)
	if err != nil {
		return &schemas.ColumnValuesResponse{
			ColumnValues: out,
			Errors:       globalColumnValuesErrors(req.Columns, noTagValues, err.Error()),
		}, nil
	}

	scopes, err := p.ds.fetchSearchTagScopes(ctx, dsInfo, defaultSearchTagsLimit)
	if err != nil {
		return &schemas.ColumnValuesResponse{
			ColumnValues: out,
			Errors:       globalColumnValuesErrors(req.Columns, noTagValues, err.Error()),
		}, nil
	}

	tagCols := tagColumnNamesSetFromScopes(scopes)
	errs := make(map[string]string)
	for _, col := range req.Columns {
		if _, skip := noTagValues[col]; skip {
			continue
		}
		if _, ok := tagCols[col]; !ok {
			continue
		}
		vals, err := p.ds.fetchTagValuesForColumn(ctx, dsInfo, col, req.TimeRange)
		if err != nil {
			p.logger.Warn("tempo schemads: tag values", "column", col, "error", err)
			errs[col] = err.Error()
			continue
		}
		out[col] = vals
	}
	if len(errs) == 0 {
		errs = nil
	}
	return &schemas.ColumnValuesResponse{ColumnValues: out, Errors: errs}, nil
}

func tableFixedColumnNamesNoTagValues(cols []schemas.Column) map[string]struct{} {
	m := make(map[string]struct{})
	for _, c := range cols {
		if c.SupportsValues != nil && *c.SupportsValues {
			continue
		}
		m[c.Name] = struct{}{}
	}
	return m
}

// globalColumnValuesErrors attaches msg to each requested column that can use tag-values,
// except fixed columns that never have that API. If there are no such columns, msg is
// returned under the empty key for schemads consumers that expect a single global error.
func globalColumnValuesErrors(columns []string, noTagValues map[string]struct{}, msg string) map[string]string {
	errs := make(map[string]string)
	for _, col := range columns {
		if noTagValues != nil {
			if _, skip := noTagValues[col]; skip {
				continue
			}
		}
		errs[col] = msg
	}
	if len(errs) == 0 {
		errs[""] = msg
	}
	return errs
}

func (p *tempoSchemaProvider) dynamicTagColumns(ctx context.Context, dsInfo *DatasourceInfo) ([]schemas.Column, error) {
	scopes, err := p.ds.fetchSearchTagScopes(ctx, dsInfo, defaultSearchTagsLimit)
	if err != nil {
		return nil, err
	}
	names := flattenTempoSearchTagScopesToColumnNames(scopes)
	truePtr := schemaBoolPtr(true)
	cols := make([]schemas.Column, 0, len(names))
	for _, n := range names {
		cols = append(cols, schemas.Column{
			Name:           n,
			Type:           schemas.ColumnTypeString,
			Operators:      traceqlStringColumnOperators(),
			Metadata:       schemas.Metadata{Description: "Tempo tag"},
			SupportsValues: truePtr,
		})
	}
	return cols, nil
}

func tagColumnNamesSetFromScopes(scopes []tempoSearchTagScope) map[string]struct{} {
	names := flattenTempoSearchTagScopesToColumnNames(scopes)
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

func flattenTempoSearchTagScopesToColumnNames(scopes []tempoSearchTagScope) []string {
	seen := make(map[string]struct{})
	for _, sc := range scopes {
		scopeName := sc.Name
		for _, t := range sc.Tags {
			if t == "" {
				continue
			}
			var col string
			if dataquery.TraceqlSearchScope(scopeName) == dataquery.TraceqlSearchScopeIntrinsic {
				col = t
			} else {
				col = scopeName + "." + t
			}
			seen[col] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
