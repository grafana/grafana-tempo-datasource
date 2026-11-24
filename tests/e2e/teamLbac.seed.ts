// Pushes the fixture traces of teamLbac.spec.ts into the Cloud Traces tenant as OTLP/HTTP JSON. The Team LBAC rule keeps
// only ALLOWED_SERVICE, so the tenant needs spans of it and of at least one other service for the rule to show.
import { randomBytes } from 'node:crypto';

import { type APIRequestContext } from '@playwright/test';

export const ALLOWED_SERVICE = 'tempo-lbac-e2e-allowed';
export const DENIED_SERVICE = 'tempo-lbac-e2e-denied';
const TRACES_PER_SERVICE = 3;

type Span = {
  traceId: string;
  spanId: string;
  name: string;
  kind: number;
  startTimeUnixNano: string;
  endTimeUnixNano: string;
};

const hex = (bytes: number): string => randomBytes(bytes).toString('hex');

// `startMs` is when the first trace starts; the traces follow one second apart.
export function buildOtlpTraces(startMs: number): { resourceSpans: unknown[] } {
  const resourceSpans = [ALLOWED_SERVICE, DENIED_SERVICE].map((service) => {
    const spans: Span[] = Array.from({ length: TRACES_PER_SERVICE }, (_, i) => {
      const start = BigInt(startMs + i * 1000) * 1_000_000n;
      return {
        traceId: hex(16),
        spanId: hex(8),
        name: `${service}-op`,
        kind: 2,
        startTimeUnixNano: start.toString(),
        endTimeUnixNano: (start + 50_000_000n).toString(),
      };
    });
    return {
      resource: { attributes: [{ key: 'service.name', value: { stringValue: service } }] },
      scopeSpans: [{ scope: { name: 'tempo-lbac-e2e' }, spans }],
    };
  });
  return { resourceSpans };
}

// `url` is the OTLP endpoint of the stack (without `/v1/traces`).
export async function seedTraces(
  request: APIRequestContext,
  { url, user, token }: { url: string; user: string; token: string }
): Promise<void> {
  // Three minutes back keeps the spans inside the windows the spec queries, which end 90 seconds ago.
  const response = await request.post(`${url.replace(/\/$/, '')}/v1/traces`, {
    headers: {
      Authorization: `Basic ${Buffer.from(`${user}:${token}`).toString('base64')}`,
      'Content-Type': 'application/json',
    },
    data: buildOtlpTraces(Date.now() - 3 * 60 * 1000),
  });
  if (!response.ok()) {
    throw new Error(`seeding fixture traces failed: HTTP ${response.status()} ${await response.text()}`);
  }
}
