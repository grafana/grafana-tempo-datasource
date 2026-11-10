import { expect, gte, test } from '@grafana/plugin-e2e';

import { runNonStreaming, runStreaming, waitForData } from './streamingDifferential.driver';
import {
  framesFromStreamedResult,
  summarize,
  traceIdsFromFrames,
  traceIdsFromStreamedTraces,
} from './streamingDifferential.helpers';
import { isCloudRun } from './env';

// Streaming queries go over gRPC and non-streaming ones over HTTP, so the two must be checked against each other.
// Both provisioned datasources point at the same Tempo; only `streamingEnabled` differs.
const STREAMING_UID = 'tempo';
const NON_STREAMING_UID = 'tempo-no-streaming';

// Closed windows that ended 90 seconds ago keep the result sets stable while the TNS apps keep emitting data. They are
// short so they stay clear of Tempo's block retention, and end on a whole second because the HTTP path truncates the
// time range to seconds while streaming doesn't. The metrics window contains the search window.
// Streaming does not start on Grafana 12.3.x: the plugin opens the Live channel with `stream`, which 12.3 does not know
// (it expects `namespace`), so the page sends no query at all.
const MIN_GRAFANA_VERSION = '12.4.0';

const SEARCH_WINDOW_MS = 30 * 1000;
const METRICS_WINDOW_MS = 10 * 60 * 1000;

// The limit is above the number of traces in the window: with fewer matches than the limit Tempo returns all of them,
// otherwise it returns an arbitrary subset that can differ between two runs.
const SEARCH_LIMIT = 1000;
const SEARCH_QUERY = { query: '{}', limit: SEARCH_LIMIT, tableType: 'traces' };
const METRICS_QUERY = '{} | rate() by (resource.service.name)';

test.describe('Streaming vs non-streaming differential', () => {
  let windowEnd: Date;

  test.beforeEach(async ({ page, grafanaVersion }) => {
    test.skip(isCloudRun, 'Relies on the local compose stack and its provisioned datasources.');
    test.skip(!gte(grafanaVersion, MIN_GRAFANA_VERSION), `Streaming needs Grafana ${MIN_GRAFANA_VERSION} or later.`);
    // On a fresh stack this can take a few minutes.
    test.setTimeout(6 * 60 * 1000);
    windowEnd = await waitForData(page, NON_STREAMING_UID, SEARCH_WINDOW_MS, 10);
  });

  test('a TraceQL search returns the same traces over gRPC and HTTP', async ({ explorePage, page }) => {
    const run = (uid: string) => ({ uid, end: windowEnd, windowMs: SEARCH_WINDOW_MS, query: SEARCH_QUERY });

    const streaming = traceIdsFromStreamedTraces(await runStreaming(page, explorePage, run(STREAMING_UID)));
    const nonStreaming = traceIdsFromFrames(
      await runNonStreaming(page, explorePage, run(NON_STREAMING_UID), '"traceID"')
    );

    expect(streaming.length).toBeGreaterThan(0);
    expect(
      nonStreaming.length,
      `the window holds ${SEARCH_LIMIT} or more traces, so Tempo may return a different subset on each run`
    ).toBeLessThan(SEARCH_LIMIT);
    expect([...new Set(streaming)].sort()).toEqual([...new Set(nonStreaming)].sort());
  });

  for (const metricsQueryType of ['range', 'instant']) {
    test(`a TraceQL ${metricsQueryType} metrics query returns the same series over gRPC and HTTP`, async ({
      explorePage,
      page,
    }) => {
      const query = {
        query: METRICS_QUERY,
        metricsQueryType,
        ...(metricsQueryType === 'range' ? { step: '60s' } : {}),
      };
      const run = (uid: string) => ({ uid, end: windowEnd, windowMs: METRICS_WINDOW_MS, query });

      const streaming = summarize(framesFromStreamedResult(await runStreaming(page, explorePage, run(STREAMING_UID))));
      const nonStreaming = summarize(
        await runNonStreaming(page, explorePage, run(NON_STREAMING_UID), 'resource.service.name')
      );

      expect(streaming.length).toBeGreaterThan(0);
      expect(streaming).toEqual(nonStreaming);
    });
  }
});
