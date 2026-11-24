import { expect, test } from '@grafana/plugin-e2e';

import { runNonStreaming, runStreaming, waitForData } from './streamingDifferential.driver';
import {
  framesFromStreamedResult,
  summarize,
  traceIdsFromFrames,
  traceIdsFromStreamedTraces,
} from './streamingDifferential.helpers';
import { isCloudRun } from './env';
import { ALLOWED_SERVICE, seedTraces } from './teamLbac.seed';
import { METRICS_QUERY, SEARCH_LIMIT, SEARCH_QUERY } from './streamingDifferential.types';

// Team LBAC is enforced only by Grafana Cloud Traces, so this runs against the managed "Tempo LBAC e2e" data sources
// of the shared Cloud stack (nightly only). The rule lets the test team see only spans of ALLOWED_SERVICE. See
// TESTING.md and grafana/data-sources docs/ds-instances/squads/oss-big-tent/oss-big-tent.md ("Tempo Team LBAC e2e").

const WINDOW_MS = 10 * 60 * 1000;

// The shared e2e admin of the Cloud run is the member of the test team.
const lbacUser = process.env.GRAFANA_ADMIN_USER?.trim();
const lbacPassword = process.env.GRAFANA_ADMIN_PASSWORD?.trim();
// Both data sources use the same traces tenant and rule; only `streamingEnabled` differs.
const STREAMING_UID = process.env.TEMPO_LBAC_DS_STREAMING_UID?.trim() || 'tempolbace2e-ds-m';
const NON_STREAMING_UID = process.env.TEMPO_LBAC_DS_NON_STREAMING_UID?.trim() || 'tempolbace2enostreaming-ds-m';

// The writer of the fixture traces: the stack ID, the OTLP gateway URL and a `traces:write` token (see TESTING.md).
const seed = {
  user: process.env.TEMPO_LBAC_SEED_USER?.trim(),
  url: process.env.TEMPO_LBAC_SEED_URL?.trim(),
  token: process.env.TEMPO_LBAC_SEED_TOKEN?.trim(),
};

test.describe('Team LBAC: streaming vs non-streaming', () => {
  // Log in with the credentials above instead of the stored admin session.
  test.use({ storageState: { cookies: [], origins: [] } });

  let windowEnd: Date;

  test.beforeAll(async ({ playwright }) => {
    if (!isCloudRun || !seed.user || !seed.url || !seed.token) {
      return;
    }
    const request = await playwright.request.newContext();
    try {
      await seedTraces(request, { user: seed.user, url: seed.url, token: seed.token });
    } finally {
      await request.dispose();
    }
  });

  test.beforeEach(async ({ page }) => {
    test.skip(!isCloudRun, 'Team LBAC is enforced only by Grafana Cloud Traces.');
    test.skip(!lbacUser || !lbacPassword, 'GRAFANA_ADMIN_USER and GRAFANA_ADMIN_PASSWORD are not set.');
    test.setTimeout(6 * 60 * 1000);

    const login = await page.request.post('/login', { data: { user: lbacUser, password: lbacPassword } });
    expect(login.ok(), `logging in as ${lbacUser} failed: HTTP ${login.status()}`).toBe(true);

    // Data visible to the team member means the fixture spans of ALLOWED_SERVICE exist.
    windowEnd = await waitForData(page, NON_STREAMING_UID, WINDOW_MS, 1);
  });

  test('a search returns the same permitted traces over gRPC and HTTP', async ({ explorePage, page }) => {
    const run = (uid: string) => ({ uid, end: windowEnd, windowMs: WINDOW_MS, query: SEARCH_QUERY });

    const streaming = traceIdsFromStreamedTraces(await runStreaming(page, explorePage, run(STREAMING_UID)));
    const nonStreaming = traceIdsFromFrames(
      await runNonStreaming(page, explorePage, run(NON_STREAMING_UID), '"traceID"')
    );

    expect(streaming.length).toBeGreaterThan(0);
    expect(nonStreaming.length, 'the window holds too many traces to compare').toBeLessThan(SEARCH_LIMIT);
    expect([...streaming].sort()).toEqual([...nonStreaming].sort());
  });

  test('a range metrics query returns the same series over gRPC and HTTP', async ({ explorePage, page }) => {
    const query = { query: METRICS_QUERY, metricsQueryType: 'range', step: '60s' };
    const run = (uid: string) => ({ uid, end: windowEnd, windowMs: WINDOW_MS, query });

    const streaming = summarize(framesFromStreamedResult(await runStreaming(page, explorePage, run(STREAMING_UID))));
    const nonStreaming = summarize(
      await runNonStreaming(page, explorePage, run(NON_STREAMING_UID), 'resource.service.name')
    );

    expect(streaming.length).toBeGreaterThan(0);
    expect(streaming).toEqual(nonStreaming);
  });

  test('the rule hides other services on both paths, and not on a data source without it', async ({
    explorePage,
    page,
  }) => {
    const query = {
      query: `{ resource.service.name != "${ALLOWED_SERVICE}" }`,
      limit: SEARCH_LIMIT,
      tableType: 'traces',
    };
    const run = (uid: string) => ({ uid, end: windowEnd, windowMs: WINDOW_MS, query });

    const streaming = traceIdsFromStreamedTraces(await runStreaming(page, explorePage, run(STREAMING_UID)));
    const nonStreaming = traceIdsFromFrames(
      await runNonStreaming(page, explorePage, run(NON_STREAMING_UID), '"frames"')
    );
    expect(streaming).toEqual([]);
    expect(nonStreaming).toEqual([]);

    // Control: the stack's default Cloud Traces data source reads the same tenant but has no rule. Without it,
    // "nothing returned" could just mean there is no other data.
    const list = await page.request.get('/api/datasources');
    expect(list.ok(), `listing data sources failed: HTTP ${list.status()}`).toBe(true);
    const control = (await list.json()).find(
      (ds: { type: string; name: string }) => ds.type === 'tempo' && /^grafanacloud-.*traces$/.test(ds.name)
    );
    expect(control, 'the default Cloud Traces data source was not found').toBeDefined();

    const response = await page.request.post('/api/ds/query', {
      data: {
        queries: [{ refId: 'A', datasource: { type: 'tempo', uid: control.uid }, queryType: 'traceql', ...query }],
        from: String(windowEnd.getTime() - WINDOW_MS),
        to: String(windowEnd.getTime()),
      },
    });
    expect(response.ok(), `control query failed: HTTP ${response.status()}`).toBe(true);
    const frames = (await response.json()).results?.A?.frames ?? [];
    expect(traceIdsFromFrames(frames).length, 'no data of other services to hide; seed the fixture').toBeGreaterThan(0);
  });
});
