package models

const PluginID = "tempo"

type SpanBarType string

const (
	// SpanBarTypeNone hides the extra span-bar label.
	SpanBarTypeNone SpanBarType = "None"
	// SpanBarTypeDuration renders the span duration (default; shown as the
	// editor placeholder even when unset).
	SpanBarTypeDuration SpanBarType = "Duration"
	// SpanBarTypeTag renders the value of the tag named by SpanBar.Tag.
	SpanBarTypeTag SpanBarType = "Tag"
)

type TraceqlSearchScope string

const (
	TraceqlSearchScopeEvent           TraceqlSearchScope = "event"
	TraceqlSearchScopeInstrumentation TraceqlSearchScope = "instrumentation"
	TraceqlSearchScopeIntrinsic       TraceqlSearchScope = "intrinsic"
	TraceqlSearchScopeLink            TraceqlSearchScope = "link"
	TraceqlSearchScopeResource        TraceqlSearchScope = "resource"
	TraceqlSearchScopeSpan            TraceqlSearchScope = "span"
	TraceqlSearchScopeUnscoped        TraceqlSearchScope = "unscoped"
)

const (
	TimeRangeForTagsLast30Minutes = 1800
	TimeRangeForTagsLast3Hours    = 10800
	TimeRangeForTagsLast24Hours   = 86400
	TimeRangeForTagsLast3Days     = 259200
	TimeRangeForTagsLast7Days     = 604800
)

type SecureJsonDataKey string

const (
	SecureJsonDataKeyBasicAuthPassword SecureJsonDataKey = "basicAuthPassword"
	SecureJsonDataKeyTLSCACert         SecureJsonDataKey = "tlsCACert"
	SecureJsonDataKeyTLSClientCert     SecureJsonDataKey = "tlsClientCert"
	SecureJsonDataKeyTLSClientKey      SecureJsonDataKey = "tlsClientKey"
)

type SecureJsonDataConfig []SecureJsonDataKey

var SecureJsonDataKeys = SecureJsonDataConfig{
	SecureJsonDataKeyBasicAuthPassword,
	SecureJsonDataKeyTLSCACert,
	SecureJsonDataKeyTLSClientCert,
	SecureJsonDataKeyTLSClientKey,
}

type StreamingEnabledConfig struct {
	// Search enables streaming for TraceQL search queries. Min Tempo: 2.2.0.
	Search bool `json:"search,omitempty"`
	// Metrics enables streaming for TraceQL metrics queries. Min Tempo: 2.7.0.
	Metrics bool `json:"metrics,omitempty"`
}

type TraceToLogsV1Config struct {
	DatasourceUID      string           `json:"datasourceUid,omitempty"`
	Tags               []string         `json:"tags,omitempty"`
	MappedTags         []TraceToLogsTag `json:"mappedTags,omitempty"`
	MapTagNamesEnabled bool             `json:"mapTagNamesEnabled,omitempty"`
	SpanStartTimeShift string           `json:"spanStartTimeShift,omitempty"`
	SpanEndTimeShift   string           `json:"spanEndTimeShift,omitempty"`
	FilterByTraceID    bool             `json:"filterByTraceID,omitempty"`
	FilterBySpanID     bool             `json:"filterBySpanID,omitempty"`
	LokiSearch         bool             `json:"lokiSearch,omitempty"`
}

type TraceToLogsTag struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

type TraceToLogsTagPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type TraceToLogsV2Config struct {
	DatasourceUID      string           `json:"datasourceUid,omitempty"`
	Tags               []TraceToLogsTag `json:"tags,omitempty"`
	SpanStartTimeShift string           `json:"spanStartTimeShift,omitempty"`
	SpanEndTimeShift   string           `json:"spanEndTimeShift,omitempty"`
	FilterByTraceID    bool             `json:"filterByTraceID,omitempty"`
	FilterBySpanID     bool             `json:"filterBySpanID,omitempty"`
	Query              string           `json:"query,omitempty"`
	CustomQuery        bool             `json:"customQuery"`
}

// TraceToMetricsQuery is one named Prometheus query offered on the
// trace→metrics link.
type TraceToMetricsQuery struct {
	Name  string `json:"name,omitempty"`
	Query string `json:"query,omitempty"`
}

// TraceToMetricsConfig is the shape written by TraceToMetricsSection.
type TraceToMetricsConfig struct {
	DatasourceUID      string                `json:"datasourceUid,omitempty"`
	Tags               []TraceToLogsTagPair  `json:"tags,omitempty"`
	Queries            []TraceToMetricsQuery `json:"queries,omitempty"`
	SpanStartTimeShift string                `json:"spanStartTimeShift,omitempty"`
	SpanEndTimeShift   string                `json:"spanEndTimeShift,omitempty"`
}

// TraceToProfilesConfig is the shape written by TraceToProfilesSection.
type TraceToProfilesConfig struct {
	DatasourceUID string           `json:"datasourceUid,omitempty"`
	Tags          []TraceToLogsTag `json:"tags,omitempty"`
	ProfileTypeID string           `json:"profileTypeId,omitempty"`
	Query         string           `json:"query,omitempty"`
	CustomQuery   bool             `json:"customQuery"`
}

// ServiceMapConfig points at the Prometheus datasource that stores the
// service-graph metrics (ServiceGraphSettings.tsx:32-37).
type ServiceMapConfig struct {
	DatasourceUID string `json:"datasourceUid,omitempty"`
}

// NodeGraphConfig toggles the node-graph view above the trace view
// (NodeGraphSettings.tsx:22-46).
type NodeGraphConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

// SpanBarConfig picks the extra label rendered next to service/operation on
// each span row (SpanBarSettings.tsx).
type SpanBarConfig struct {
	Type SpanBarType `json:"type,omitempty"`
	Tag  string      `json:"tag,omitempty"`
}

// TraceqlFilter is a single static TraceQL search filter (dataquery.ts:115-142).
type TraceqlFilter struct {
	// ID uniquely identifies the filter within the editor; not used in query generation.
	ID            string             `json:"id"`
	IsCustomValue bool               `json:"isCustomValue,omitempty"`
	Operator      string             `json:"operator,omitempty"`
	Scope         TraceqlSearchScope `json:"scope,omitempty"`
	Tag           string             `json:"tag,omitempty"`
	// Value is either a single string or a []string depending on the operator.
	// Modeled as `any` so the raw shape round-trips through LoadConfig.
	Value     any    `json:"value,omitempty"`
	ValueType string `json:"valueType,omitempty"`
}

// SearchConfig is the shape written by TraceQLSearchSettings.tsx:28-46.
type SearchConfig struct {
	// Hide removes the TraceQL search tab from the query editor.
	Hide bool `json:"hide,omitempty"`
	// Filters are pre-configured static filters exposed in the search UI.
	Filters []TraceqlFilter `json:"filters,omitempty"`
}

// TraceQueryConfig is the shape written by QuerySettings.tsx (TraceID query
// time-range shifts).
type TraceQueryConfig struct {
	TimeShiftEnabled   bool   `json:"timeShiftEnabled,omitempty"`
	SpanStartTimeShift string `json:"spanStartTimeShift,omitempty"`
	SpanEndTimeShift   string `json:"spanEndTimeShift,omitempty"`
}

// Config is the fully loaded configuration of a Tempo datasource instance.
//
// The Tempo backend's server-side settings reads are minimal:
//   - pkg/tempo/tempo.go:52-90 (NewDatasource) — reads settings.URL and delegates the
//     rest to settings.HTTPClientOptions (SDK) and newGrpcClient.
//   - pkg/tempo/tempo.go:150-208 (CheckHealth) — decodes JSONData as
//     map[string]interface{} to probe streamingEnabled.search.
//   - pkg/tempo/grpc.go:85-137 — reads settings.URL, settings.BasicAuthEnabled,
//     and settings.ProxyClient to configure the gRPC streaming client.
//
// The Tempo plugin ships no pkg/models/settings.go, so the jsonData shape on
// this struct is the intended settings model: it mirrors what the editor
// writes and what a Grafana-side caller needs to know about a Tempo
// datasource instance.
type Config struct {
	// Root-level fields (json:"-" so they are not part of jsonData). URL is
	// read by both the HTTP client (pkg/tempo/tempo.go:78) and the gRPC client
	// (pkg/tempo/grpc.go:86); BasicAuth / BasicAuthUser / WithCredentials are
	// consumed by settings.HTTPClientOptions() at pkg/tempo/tempo.go:54 and
	// BasicAuth also gates gRPC per-RPC credentials (pkg/tempo/grpc.go:178-184).
	URL             string `json:"-"`
	BasicAuth       bool   `json:"-"`
	BasicAuthUser   string `json:"-"`
	WithCredentials bool   `json:"-"`

	// jsonData fields — the subset the editor writes and/or the SDK reads.
	// Custom HTTP header pairs (jsonData.httpHeaderName<N> /
	// secureJsonData.httpHeaderValue<N>) are not modeled here because they are
	// dynamically indexed.
	TLSAuth           bool     `json:"tlsAuth,omitempty"`
	TLSAuthWithCACert bool     `json:"tlsAuthWithCACert,omitempty"`
	TLSSkipVerify     bool     `json:"tlsSkipVerify,omitempty"`
	ServerName        string   `json:"serverName,omitempty"`
	Timeout           float64  `json:"timeout,omitempty"`
	KeepCookies       []string `json:"keepCookies,omitempty"`
	OauthPassThru     bool     `json:"oauthPassThru,omitempty"`

	StreamingEnabled StreamingEnabledConfig `json:"streamingEnabled,omitempty"`

	TracesToLogsV2 TraceToLogsV2Config `json:"tracesToLogsV2,omitempty"`
	TracesToLogs   TraceToLogsV1Config `json:"tracesToLogs,omitempty"`

	TracesToMetrics  TraceToMetricsConfig  `json:"tracesToMetrics,omitempty"`
	TracesToProfiles TraceToProfilesConfig `json:"tracesToProfiles,omitempty"`

	ServiceMap ServiceMapConfig `json:"serviceMap,omitempty"`
	NodeGraph  NodeGraphConfig  `json:"nodeGraph,omitempty"`
	SpanBar    SpanBarConfig    `json:"spanBar,omitempty"`

	Search SearchConfig `json:"search,omitempty"`

	TraceQuery TraceQueryConfig `json:"traceQuery,omitempty"`

	// TimeRangeForTags is one of the five allowed second-counts (default 1800).
	TimeRangeForTags int64 `json:"timeRangeForTags,omitempty"`

	// TagLimit — TagLimitSettings.tsx writes v.currentTarget.value from an
	// Input type="number", so the persisted value is technically a string in
	// storage. We accept both by using json.Number-style tolerant parsing in
	// UnmarshalJSON below.
	TagLimit int64 `json:"tagLimit,omitempty"`

	// DecryptedSecureJSONData holds the decrypted secure values by key
	// (basicAuthPassword, tlsCACert, tlsClientCert, tlsClientKey).
	DecryptedSecureJSONData map[SecureJsonDataKey]string `json:"-"`
}
