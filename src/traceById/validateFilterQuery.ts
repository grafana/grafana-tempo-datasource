import {
  AttributeField,
  Event,
  Identifier,
  Instrumentation,
  IntrinsicField,
  Link,
  parser,
  Resource,
  Span,
  SpansetFilter,
  SpansetPipeline,
  SpansetPipelineExpression,
} from '@grafana/lezer-traceql';
import { type SyntaxNode } from '@lezer/common';

import { getErrorNodes } from '../traceql/highlighting';

const ALLOWED_ATTRIBUTE_SCOPES = new Set<number>([Resource, Span, Event, Link, Instrumentation]);

// Mirrors matchSpansSupportedIntrinsics in grafana/tempo's spanset_filter_match.go, including
// every bare/scoped alias (e.g. "duration" and "span:duration" resolve to the same intrinsic).
const ALLOWED_INTRINSICS = new Set<string>([
  'duration',
  'span:duration',
  'name',
  'span:name',
  'status',
  'span:status',
  'statusMessage',
  'span:statusMessage',
  'kind',
  'span:kind',
  'span:id',
  'span:parentID',
  'event:name',
  'event:timeSinceStart',
  'link:spanID',
  'link:traceID',
  'instrumentation:name',
  'instrumentation:version',
]);

export function validateFilterQuery(query: string): string | undefined {
  if (query.trim() === '') {
    return undefined;
  }

  if (getErrorNodes(query).length > 0) {
    return 'Invalid TraceQL syntax.';
  }

  const topExpression = parser.parse(query).topNode.firstChild;
  if (!topExpression || topExpression.type.id !== SpansetPipelineExpression || topExpression.nextSibling) {
    return 'Only a single spanset filter is supported, e.g. { status = error }.';
  }

  const pipeline = topExpression.firstChild;
  if (!pipeline || pipeline.type.id !== SpansetPipeline || pipeline.nextSibling) {
    return 'Pipelines and combined spansets are not supported here.';
  }

  const filter = pipeline.firstChild;
  if (!filter || filter.type.id !== SpansetFilter || filter.nextSibling) {
    return 'Pipelines and combined spansets are not supported here.';
  }

  return findScopeError(filter, query);
}

function findScopeError(node: SyntaxNode, query: string): string | undefined {
  if (node.type.id === AttributeField) {
    const scope = node.firstChild;
    if (scope && scope.type.id !== Identifier && !ALLOWED_ATTRIBUTE_SCOPES.has(scope.type.id)) {
      const text = query.slice(node.from, node.to);
      return `Unsupported attribute scope in "${text}" — use resource., span., event., link., instrumentation., or no scope.`;
    }
  }

  if (node.type.id === IntrinsicField) {
    const text = query.slice(node.from, node.to);
    if (!ALLOWED_INTRINSICS.has(text)) {
      return `Unsupported field "${text}" — use resource., span., event., link., instrumentation., no scope, or a supported intrinsic (duration, name, status, statusMessage, kind, span:id, span:parentID, event:name, event:timeSinceStart, link:spanID, link:traceID, instrumentation:name, instrumentation:version).`;
    }
  }

  for (let child = node.firstChild; child; child = child.nextSibling) {
    const error = findScopeError(child, query);
    if (error) {
      return error;
    }
  }

  return undefined;
}
