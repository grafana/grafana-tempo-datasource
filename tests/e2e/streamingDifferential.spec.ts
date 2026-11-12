import { expect, test } from '@grafana/plugin-e2e';

import { runNonStreaming, runStreaming, waitForData } from './streamingDifferential.driver';
import {
  framesFromStreamedResult,
  summarize,
  traceIdsFromFrames,
  traceIdsFromStreamedTraces,
} from './streamingDifferential.helpers';
import { isCloudRun } from './env';
import {
  METRICS_QUERY,
  METRICS_WINDOW_MS,
  NON_STREAMING_UID,
  SEARCH_LIMIT,
  SEARCH_QUERY,
  SEARCH_WINDOW_MS,
  STREAMING_UID,
} from './streamingDifferential.types';

// Streaming queries go over gRPC and non-streaming ones over HTTP, so the two must be checked against each other.

test.describe('Streaming vs non-streaming differential', () => {
  let windowEnd: Date;

  test.beforeEach(async ({ page }) => {
    test.skip(isCloudRun, 'Relies on the local compose stack and its provisioned datasources.');
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
    expect([...streaming].sort()).toEqual([...nonStreaming].sort());
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
