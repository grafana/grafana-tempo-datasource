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

const (
	tempoSchemadsTableSpans = "spans"

	tempoSpanColTraceIDHidden = "traceIdHidden"
	tempoSpanColTraceService  = "traceService"
	tempoSpanColTraceName     = "traceName"
	tempoSpanColSpanID        = "spanID"
	tempoSpanColTime          = "time"
	tempoSpanColName          = "name"
	tempoSpanColDuration      = "duration"
)

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

func spansFixedColumnNames() map[string]struct{} {
	m := make(map[string]struct{})
	for _, c := range spansFixedColumns() {
		m[c.Name] = struct{}{}
	}
	return m
}

func spansFixedColumns() []schemas.Column {
	falsePtr := schemaBoolPtr(false)
	truePtr := schemaBoolPtr(true)
	traceqlStringOps := traceqlStringColumnOperators()
	traceqlIDOps := traceqlIdentifierColumnOperators()
	traceqlDurOps := traceqlDurationColumnOperators()
	return []schemas.Column{
		{
			Name: tempoSpanColTraceIDHidden, Type: schemas.ColumnTypeString, Operators: traceqlIDOps,
			Description: "Trace ID (TraceQL: trace:id). Used for drill-down links.", SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColTraceService, Type: schemas.ColumnTypeString, Operators: traceqlStringOps,
			Description: "Root trace service (TraceQL: trace:rootService / rootServiceName).", SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColTraceName, Type: schemas.ColumnTypeString, Operators: traceqlStringOps,
			Description: "Root trace name (TraceQL: trace:rootName / rootName).", SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColSpanID, Type: schemas.ColumnTypeString, Operators: traceqlIDOps,
			Description: "Span ID (TraceQL: span:id).", SupportsValues: falsePtr,
		},
		{
			// Span start time is not a TraceQL filter intrinsic; bound queries with the panel time range.
			Name: tempoSpanColTime, Type: schemas.ColumnTypeDatetime, Operators: nil,
			Description: "Span start time (display only). Not filterable in TraceQL; use the query time range.", SupportsValues: falsePtr,
		},
		{
			Name: tempoSpanColName, Type: schemas.ColumnTypeString, Operators: traceqlStringOps,
			Description: "Span name (TraceQL: name / span:name).", SupportsValues: truePtr,
		},
		{
			Name: tempoSpanColDuration, Type: schemas.ColumnTypeFloat64, Operators: traceqlDurOps,
			Description: "Span duration (TraceQL: duration / span:duration). Comparisons use duration units in the query (for example 100ms).", SupportsValues: truePtr,
		},
	}
}

func schemaBoolPtr(b bool) *bool {
	return &b
}

// mergeSpansColumnsUnique returns fixed span columns followed by dynamic tag columns,
// omitting any dynamic column whose Name collides with a fixed column (e.g. intrinsic
// "name" / "duration" from Tempo search tags API vs the same keys in spansFixedColumns).
func mergeSpansColumnsUnique(fixed, dynamic []schemas.Column) []schemas.Column {
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

func spansMetricsResultColumns() []schemas.Column {
	return []schemas.Column{
		{Name: "timestamp", Type: schemas.ColumnTypeDatetime},
		{Name: "value", Type: schemas.ColumnTypeFloat64},
	}
}

func spansTableHints() []schemas.TableHint {
	return []schemas.TableHint{
		{Name: "step", Description: "Metrics query step/resolution, e.g. step('30s')", HasValue: true},
		{Name: "instant", Description: "Run as instant TraceQL metrics query"},
		{Name: "exemplars", Description: "Max exemplars for range metrics (0 = none)", HasValue: true},
	}
}

var tempoMetricsCapabilities = &schemas.DatasourceCapabilities{
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
	cols := mergeSpansColumnsUnique(spansFixedColumns(), tagCols)
	cols = append(cols, spansMetricsResultColumns()...)
	table := schemas.Table{
		Name:       tempoSchemadsTableSpans,
		Columns:    cols,
		TableHints: spansTableHints(),
	}
	resp := &schemas.SchemaResponse{
		FullSchema: &schemas.Schema{
			Tables:       []schemas.Table{table},
			Capabilities: tempoMetricsCapabilities,
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
		return &schemas.TablesResponse{Errors: map[string]string{tempoSchemadsTableSpans: err.Error()}}, nil
	}
	return &schemas.TablesResponse{
		Tables:       []string{tempoSchemadsTableSpans},
		Capabilities: tempoMetricsCapabilities,
	}, nil
}

// Columns implements schemas.ColumnsHandler.
func (p *tempoSchemaProvider) Columns(ctx context.Context, req *schemas.ColumnsRequest) (*schemas.ColumnsResponse, error) {
	out := make(map[string][]schemas.Column, len(req.Tables))
	var errs map[string]string
	wantSpans := false
	for _, t := range req.Tables {
		if t == tempoSchemadsTableSpans {
			wantSpans = true
			continue
		}
		if errs == nil {
			errs = make(map[string]string)
		}
		errs[t] = unsupportedSchemadsTableError(t)
	}

	if !wantSpans {
		return &schemas.ColumnsResponse{Columns: out, Errors: errs}, nil
	}

	dsInfo, err := p.dsInfo(ctx)
	if err != nil {
		if errs == nil {
			errs = make(map[string]string)
		}
		errs[tempoSchemadsTableSpans] = err.Error()
		return &schemas.ColumnsResponse{Columns: out, Errors: errs}, nil
	}

	tagCols, tagErr := p.dynamicTagColumns(ctx, dsInfo)
	if tagErr != nil {
		p.logger.Warn("tempo schemads: failed to load tags for columns", "error", tagErr)
	}

	out[tempoSchemadsTableSpans] = mergeSpansColumnsUnique(spansFixedColumns(), tagCols)
	if tagErr != nil {
		if errs == nil {
			errs = make(map[string]string)
		}
		errs[tempoSchemadsTableSpans] = fmt.Sprintf("attribute columns unavailable: %v", tagErr)
	}
	return &schemas.ColumnsResponse{Columns: out, Errors: errs}, nil
}

func unsupportedSchemadsTableError(table string) string {
	return fmt.Sprintf("unsupported table %q (only %q is supported)", table, tempoSchemadsTableSpans)
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
	if req.Table != tempoSchemadsTableSpans {
		return &schemas.ColumnValuesResponse{
			ColumnValues: out,
			Errors:       globalColumnValuesErrors(req.Columns, unsupportedSchemadsTableError(req.Table)),
		}, nil
	}

	dsInfo, err := p.dsInfo(ctx)
	if err != nil {
		return &schemas.ColumnValuesResponse{
			ColumnValues: out,
			Errors:       globalColumnValuesErrors(req.Columns, err.Error()),
		}, nil
	}

	scopes, err := p.ds.fetchSearchTagScopes(ctx, dsInfo, defaultSearchTagsLimit)
	if err != nil {
		return &schemas.ColumnValuesResponse{
			ColumnValues: out,
			Errors:       globalColumnValuesErrors(req.Columns, err.Error()),
		}, nil
	}

	tagCols := tagColumnNamesSetFromScopes(scopes)
	noTagValues := spansFixedColumnNamesNoTagValues()
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

// spansFixedColumnNamesNoTagValues is the set of fixed span columns that never use the
// Tempo tag-values API. It is derived from spansFixedColumns (SupportsValues == false)
// so metadata and ColumnValues stay aligned.
func spansFixedColumnNamesNoTagValues() map[string]struct{} {
	m := make(map[string]struct{})
	for _, c := range spansFixedColumns() {
		if c.SupportsValues != nil && *c.SupportsValues {
			continue
		}
		m[c.Name] = struct{}{}
	}
	return m
}

// globalColumnValuesErrors attaches msg to each requested column that can use tag-values,
// except fixed columns that never have that API (see spansFixedColumnNamesNoTagValues).
// If there are no such columns, msg is returned under the empty key for schemads consumers
// that expect a single global error.
func globalColumnValuesErrors(columns []string, msg string) map[string]string {
	noTag := spansFixedColumnNamesNoTagValues()
	errs := make(map[string]string)
	for _, col := range columns {
		if _, skip := noTag[col]; skip {
			continue
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
	fixedNames := spansFixedColumnNames()
	names := flattenTempoSearchTagScopesToColumnNames(scopes)
	truePtr := schemaBoolPtr(true)
	cols := make([]schemas.Column, 0, len(names))
	for _, n := range names {
		if _, isFixed := fixedNames[n]; isFixed {
			continue
		}
		cols = append(cols, schemas.Column{
			Name:           n,
			Type:           schemas.ColumnTypeString,
			Operators:      traceqlStringColumnOperators(),
			Description:    "Attribute tag from Tempo.",
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
