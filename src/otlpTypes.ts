/**
 * Minimal local type definitions for the (legacy) OpenTelemetry Protocol (OTLP)
 * JSON wire format used when converting between Grafana trace data frames and
 * Tempo's OTLP trace endpoints.
 *
 * These previously came from `@opentelemetry/exporter-collector`, a package
 * that was retired years ago and pulled in a very old, vulnerable version of
 * `@opentelemetry/core` (CVE-2026-54285) as a transitive dependency. We only
 * ever used its TypeScript types here -- never any of its runtime/export
 * code -- so rather than pull in an unrelated (and much heavier) modern OTel
 * SDK package just for type-checking, we vendor the minimal subset of the
 * OTLP shape this datasource actually reads and writes.
 *
 * Note: Tempo's OTLP JSON payloads use the pre-1.0 OTLP field names
 * (`instrumentationLibrarySpans` / `instrumentationLibrary`) rather than the
 * `scopeSpans` / `scope` naming used by more recent OTLP proto versions, so
 * these types intentionally match that legacy shape to keep behavior
 * identical to before.
 */

export interface AnyValue {
  stringValue?: string;
  boolValue?: boolean;
  intValue?: number | string;
  doubleValue?: number;
  arrayValue?: {
    values: AnyValue[];
  };
}

export interface KeyValue {
  key: string;
  value: AnyValue;
}

export interface Resource {
  attributes: KeyValue[];
  droppedAttributesCount?: number;
}

export interface InstrumentationLibrary {
  name?: string;
  version?: string;
}

export interface SpanStatus {
  code?: number;
  message?: string;
}

export interface SpanEvent {
  timeUnixNano: number;
  name: string;
  attributes?: KeyValue[];
  droppedAttributesCount?: number;
}

export interface SpanLink {
  traceId: string;
  spanId: string;
  traceState?: string;
  attributes?: KeyValue[];
  droppedAttributesCount?: number;
}

export interface Span {
  traceId: string;
  spanId: string;
  traceState?: string;
  parentSpanId?: string;
  name?: string;
  kind?: number | string;
  startTimeUnixNano?: number;
  endTimeUnixNano?: number;
  attributes?: KeyValue[];
  droppedAttributesCount?: number;
  events?: SpanEvent[];
  droppedEventsCount?: number;
  links?: SpanLink[];
  droppedLinksCount?: number;
  status?: SpanStatus;
}

export interface InstrumentationLibrarySpans {
  instrumentationLibrary?: InstrumentationLibrary;
  spans: Span[];
}

export interface ResourceSpans {
  resource?: Resource;
  instrumentationLibrarySpans: InstrumentationLibrarySpans[];
}
