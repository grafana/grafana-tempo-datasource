import { expect, test } from '@grafana/plugin-e2e';
import { type APIRequestContext } from '@playwright/test';

import { runNonStreaming, runStreaming, waitForData } from './streamingDifferential.driver';
import { isQueryUrl, traceIdsFromFrames, traceIdsFromStreamedTraces } from './streamingDifferential.helpers';
import { isCloudRun } from './env';
import { ALLOWED_SERVICE, type SeededTraceIds, seedTraces } from './teamLbac.seed';
import { SEARCH_LIMIT, SEARCH_QUERY } from './streamingDifferential.types';

// Team LBAC is enforced only by Grafana Cloud Traces, so this runs against two dedicated data sources of the shared
// Cloud stack (nightly only). The rule lets the test team see only spans of ALLOWED_SERVICE. See TESTING.md.

const WINDOW_MS = 10 * 60 * 1000;

// The shared e2e admin of the Cloud run is the member of the test team.
const lbacUser = process.env.GRAFANA_ADMIN_USER?.trim();
const lbacPassword = process.env.GRAFANA_ADMIN_PASSWORD?.trim();
// Both data sources use the same traces tenant and rule; only `streamingEnabled` differs.
const STREAMING_UID = process.env.TEMPO_LBAC_DS_STREAMING_UID?.trim() || 'tempolbace2e-ds-m';
const NON_STREAMING_UID = process.env.TEMPO_LBAC_DS_NON_STREAMING_UID?.trim() || 'tempolbace2enostreaming-ds-m';

// The writer of the fixture traces: user, OTLP URL and a write-only token (see TESTING.md).
const seed = {
  user: process.env.TEMPO_LBAC_SEED_USER?.trim(),
  url: process.env.TEMPO_LBAC_SEED_URL?.trim(),
  token: process.env.TEMPO_LBAC_SEED_TOKEN?.trim(),
};

// The trace ids that a search over the window returns for `uid` as the signed-in user.
async function visibleTraceIds(request: APIRequestContext, uid: string, query: string, end: Date): Promise<string[]> {
  const response = await request.post('/api/ds/query', {
    data: {
      queries: [
        {
          refId: 'A',
          datasource: { type: 'tempo', uid },
          queryType: 'traceql',
          query,
          limit: SEARCH_LIMIT,
          tableType: 'traces',
        },
      ],
      from: String(end.getTime() - WINDOW_MS),
      to: String(end.getTime()),
    },
  });
  expect(response.ok(), `search over ${uid} failed: HTTP ${response.status()}`).toBe(true);
  return traceIdsFromFrames((await response.json()).results?.A?.frames ?? []);
}

test.describe('Team LBAC: streaming vs non-streaming', () => {
  // Log in with the credentials above instead of the stored admin session.
  test.use({ storageState: { cookies: [], origins: [] } });

  let windowEnd: Date;
  let seeded: SeededTraceIds | undefined;
  let trail: string[] = [];

  test.beforeAll(async ({ playwright }) => {
    if (!isCloudRun || !seed.user || !seed.url || !seed.token) {
      return;
    }
    const request = await playwright.request.newContext();
    try {
      seeded = await seedTraces(request, { user: seed.user, url: seed.url, token: seed.token });
    } finally {
      await request.dispose();
    }
  });

  test.beforeEach(async ({ page }) => {
    test.skip(!isCloudRun, 'Team LBAC is enforced only by Grafana Cloud Traces.');
    test.skip(!lbacUser || !lbacPassword, 'GRAFANA_ADMIN_USER and GRAFANA_ADMIN_PASSWORD are not set.');
    test.setTimeout(6 * 60 * 1000);

    // Kept for the failure diagnostics below: where the page went and how each query answered.
    trail = [];
    page.on('framenavigated', (frame) => {
      if (frame === page.mainFrame()) {
        trail.push(`nav ${new URL(frame.url()).pathname}`);
      }
    });
    page.on('response', async (response) => {
      if (!isQueryUrl(response.url())) {
        return;
      }
      const body = await response.text().catch(() => '');
      const error =
        /"error":"([^"]{0,100})/
          .exec(body)?.[1]
          ?.replace(/https?:\/\/\S+/g, '<url>')
          .replace(/\d{4,}/g, '<n>') ?? '';
      let keys = '';
      try {
        keys = Object.keys(JSON.parse(body)).slice(0, 5).join(',');
      } catch {
        // Not JSON; nothing to list.
      }
      trail.push(`query ${response.status()}${keys ? ` keys=${keys}` : ''}${error ? ` error="${error}"` : ''}`);
    });

    const login = await page.request.post('/login', { data: { user: lbacUser, password: lbacPassword } });
    expect(login.ok(), `logging in as ${lbacUser} failed: HTTP ${login.status()}`).toBe(true);

    // Data visible to the team member means the fixture spans of ALLOWED_SERVICE exist.
    windowEnd = await waitForData(page, NON_STREAMING_UID, WINDOW_MS, 1);

    // Both paths are queried one after the other, so every trace pushed for this run has to be searchable first.
    // Otherwise the path that runs first can miss traces that the second one sees.
    const pushed = seeded;
    if (pushed) {
      await expect
        .poll(
          async () => {
            const visible = await visibleTraceIds(page.request, NON_STREAMING_UID, '{}', windowEnd);
            return pushed.allowed.filter((id) => !visible.includes(id));
          },
          { timeout: 3 * 60 * 1000, intervals: [5_000], message: 'the fixture traces did not become searchable' }
        )
        .toEqual([]);
    }
  });

  // The Actions log of this repository is public, so print only what shows how far the page got, never page text.
  test.afterEach(async ({ page }, testInfo) => {
    if (testInfo.status === testInfo.expectedStatus) {
      return;
    }
    const state = await page
      .evaluate(() => ({
        path: location.pathname,
        title: document.title,
        radios: document.querySelectorAll('input[type="radio"], [role="radio"]').length,
        loginForm: !!document.querySelector('input[name="user"], input[name="password"]'),
        dialogs: Array.from(document.querySelectorAll('[role="dialog"] h1, [role="dialog"] h2')).map((h) =>
          (h.textContent ?? '').slice(0, 80)
        ),
        alerts: document.querySelectorAll('[role="alert"]').length,
      }))
      .catch((error: unknown) => ({ evaluateFailed: String(error).slice(0, 120) }));
    console.log(
      `teamLbac diagnostics for "${testInfo.title}": ${JSON.stringify({ ...state, trail: trail.slice(-12) })}`
    );
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

    await expect
      .poll(async () => (await visibleTraceIds(page.request, control.uid, query.query, windowEnd)).length, {
        timeout: 2 * 60 * 1000,
        intervals: [5_000],
        message: 'no data of other services to hide; seed the fixture',
      })
      .toBeGreaterThan(0);
  });
});
