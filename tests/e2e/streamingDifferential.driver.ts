// Drives Grafana for streamingDifferential.spec.ts: waits for data, runs a query in Explore and returns what the
// non-streaming and the streaming path produced. The parsing is in streamingDifferential.helpers.ts, which has unit tests.
import { expect, type ExplorePage } from '@grafana/plugin-e2e';
import { type Page } from '@playwright/test';

import {
  type Frame,
  type LiveColumns,
  finalLiveResult,
  parseLiveMessages,
  secondsAgo,
  traceIdsFromFrames,
} from './streamingDifferential.helpers';

// How long a single wait may take: a response, a click, the end of a stream.
const ACTION_TIMEOUT_MS = 30_000;

// A query to run: the datasource, where the window ends, how long it is, and the query fields.
export type QueryRun = { uid: string; end: Date; windowMs: number; query: Record<string, unknown> };

// The TNS trace generators start together with the stack, and the windows end 90 seconds ago, so right after
// `docker compose up` there is nothing in them yet. Waits until `windowMs` before that moment holds at least `min`
// traces and returns where the window ends.
export async function waitForData(page: Page, uid: string, windowMs: number, min: number): Promise<Date> {
  const traceCount = async (): Promise<number> => {
    const end = secondsAgo(90);
    const response = await page.request.post('/api/ds/query', {
      data: {
        queries: [
          {
            refId: 'A',
            datasource: { type: 'tempo', uid },
            queryType: 'traceql',
            query: '{}',
            limit: 1000,
            tableType: 'traces',
          },
        ],
        from: String(end.getTime() - windowMs),
        to: String(end.getTime()),
      },
    });
    const frames: Frame[] = (await response.json()).results?.A?.frames ?? [];
    return traceIdsFromFrames(frames).length;
  };

  await expect
    .poll(traceCount, {
      timeout: 5 * 60 * 1000,
      intervals: [5_000],
      message: 'Tempo has no traces to compare; are the trace generators running?',
    })
    .toBeGreaterThanOrEqual(min);
  return secondsAgo(90);
}

function exploreParams({ uid, end, windowMs, query }: QueryRun): URLSearchParams {
  const panes = JSON.stringify({
    explore: {
      datasource: uid,
      queries: [{ refId: 'A', datasource: { type: 'tempo', uid }, queryType: 'traceql', ...query }],
      range: { from: new Date(end.getTime() - windowMs).toISOString(), to: end.toISOString() },
    },
  });
  return new URLSearchParams({ orgId: '1', schemaVersion: '1', panes });
}

// Opens Explore with the query and runs it. Clicking before the query editor is mounted does not run the query.
async function runQuery(page: Page, explorePage: ExplorePage, run: QueryRun): Promise<void> {
  await explorePage.goto({ queryParams: exploreParams(run) });
  await expect(page.getByRole('radio', { name: 'TraceQL', exact: true })).toBeChecked();
  await page
    .getByRole('button', { name: /run query/i })
    .first()
    .click({ timeout: ACTION_TIMEOUT_MS });
}

// The non-streaming response: frames from the /api/ds/query response that contains `marker`.
export async function runNonStreaming(
  page: Page,
  explorePage: ExplorePage,
  run: QueryRun,
  marker: string
): Promise<Frame[]> {
  // The test timeout is long because waitForData can take minutes, so the waits below have their own, shorter limits.
  const responsePromise = page.waitForResponse(
    async (r) => {
      if (!r.url().includes('/api/ds/query') || !r.ok()) {
        return false;
      }
      // The body can be gone for responses of cancelled or superseded requests.
      return (await r.text().catch(() => '')).includes(marker);
    },
    { timeout: ACTION_TIMEOUT_MS }
  );
  await runQuery(page, explorePage, run);
  return (await (await responsePromise).json()).results.A.frames;
}

// The streaming response: the Grafana Live messages. The last one in state "done" carries the final result.
export async function runStreaming(page: Page, explorePage: ExplorePage, run: QueryRun): Promise<unknown> {
  const messages: LiveColumns[] = [];
  page.on('websocket', (ws) => ws.on('framereceived', (f) => messages.push(...parseLiveMessages(String(f.payload)))));
  await runQuery(page, explorePage, run);

  await expect.poll(() => finalLiveResult(messages)?.state, { timeout: ACTION_TIMEOUT_MS }).toBeDefined();
  const final = finalLiveResult(messages);
  expect(final?.error, 'the stream ended with an error').toBe('');
  return final?.result;
}
