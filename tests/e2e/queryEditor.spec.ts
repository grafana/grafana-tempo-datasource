/// <reference types="node" />
import { expect, test } from '@grafana/plugin-e2e';
import { type Locator, type Page } from '@playwright/test';

const DS_NAME = process.env.DS_INSTANCE_NAME || 'Tempo';
const DS_UID = 'tempo';

// Tempo's docker-compose stack (TNS db/app/loadgen) continuously emits traces
// via the Jaeger SDK, so we always have fresh data within the last few minutes.
// Compute the query window once at module load so every test in this run uses
// the same range. A 1 h lookback comfortably contains all live traces.
const DYNAMIC_TO_ISO = new Date().toISOString();
const DYNAMIC_FROM_ISO = new Date(Date.now() - 60 * 60 * 1000).toISOString();

// Grafana 13 migrated query editor row selectors from aria-label to data-testid
// (https://github.com/grafana/grafana/pull/121784). This helper matches both
// shapes so tests work across versions until @grafana/plugin-e2e ships a fix
// and this repo upgrades.
function getQueryEditorRow(page: Page, refId: string): Locator {
  return page.locator('[data-testid="data-testid Query editor row"], [aria-label="Query editor row"]').filter({
    has: page.locator(
      `[data-testid="data-testid Query editor row title ${refId}"], [aria-label="Query editor row title ${refId}"]`
    ),
  });
}

// Builds an Explore URL with a Tempo query pre-encoded in the panes parameter.
// Uses the computed ISO timestamps so the query lands within the live trace
// window. `query` is the TraceQL string for queryType=traceql; ignored for
// other query types.
function exploreUrl(queryType: string, extra: Record<string, unknown> = {}): string {
  const panes = JSON.stringify({
    explore: {
      datasource: DS_UID,
      queries: [
        {
          refId: 'A',
          datasource: { type: 'tempo', uid: DS_UID },
          queryType,
          ...extra,
        },
      ],
      range: { from: DYNAMIC_FROM_ISO, to: DYNAMIC_TO_ISO },
    },
  });
  return `/explore?orgId=1&schemaVersion=1&panes=${encodeURIComponent(panes)}`;
}

// Switches the Tempo query type by clicking the radio button. Never relies on
// the URL to set the query type — the URL is read on first load only and the
// editor re-renders when a different radio is checked.
async function switchQueryType(page: Page, name: 'Search' | 'TraceQL' | 'Service Graph'): Promise<void> {
  const radio = getQueryEditorRow(page, 'A').getByRole('radio', { name, exact: true });
  await radio.click();
  await expect(radio).toBeChecked();
}

test.describe('Query editor', () => {
  test.beforeEach(async ({ explorePage }) => {
    // explorePage.goto() is called by the fixture before this hook runs.
    // Tempo is provisioned as the default — datasource.set() confirms the
    // selection without firing a new query (Grafana treats it as a no-op
    // when unchanged).
    await explorePage.datasource.set(DS_NAME);
  });

  test.describe('rendering', () => {
    test('smoke: renders TraceQL query type tabs', { tag: '@plugins' }, async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      // The Tempo query editor exposes a RadioButtonGroup with three modes.
      // The "Import trace" button next to the radios opens a modal for
      // uploading a trace JSON; it is not its own query type.
      await expect(queryRow.getByRole('radio', { name: 'Search', exact: true })).toBeVisible({ timeout: 30_000 });
      await expect(queryRow.getByRole('radio', { name: 'TraceQL', exact: true })).toBeVisible();
      await expect(queryRow.getByRole('radio', { name: 'Service Graph', exact: true })).toBeVisible();
      await expect(queryRow.getByRole('button', { name: 'Import trace' })).toBeVisible();
    });

    test('Search mode shows TraceQL filter controls', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await switchQueryType(page, 'Search');
      // TraceqlSearch renders the canonical Service Name / Span Name / Status /
      // Duration / Tags filters as inline labels.
      await expect(queryRow.getByText('Service Name', { exact: true })).toBeVisible();
      await expect(queryRow.getByText('Span Name', { exact: true })).toBeVisible();
      await expect(queryRow.getByText('Duration', { exact: true })).toBeVisible();
      // The "Edit in TraceQL" button copies the visual filters into the
      // TraceQL Monaco editor — assert it renders so we know the read-only
      // expression preview mounted.
      await expect(queryRow.getByRole('button', { name: 'Edit in TraceQL' })).toBeVisible();
    });

    test('TraceQL mode shows Monaco editor', async ({ page }) => {
      await switchQueryType(page, 'TraceQL');
      const queryRow = getQueryEditorRow(page, 'A');
      // Monaco renders a single multi-line textbox with the
      // "Editor content" aria-label.
      await expect(queryRow.getByRole('textbox', { name: /Editor content/ })).toBeVisible();
    });

    test('Service Graph mode shows the Service Graph editor', async ({ page }) => {
      await switchQueryType(page, 'Service Graph');
      const queryRow = getQueryEditorRow(page, 'A');
      // Service Graph mode replaces the search filters with a service-name
      // textbox and (optionally) a Prometheus query-builder row. Asserting on
      // the radio selection alone is enough to know the editor remounted —
      // the visible search filters from the Search panel must be gone.
      await expect(queryRow.getByText('Service Name', { exact: true })).toHaveCount(0);
    });

    test('Import trace opens the upload modal', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await queryRow.getByRole('button', { name: 'Import trace' }).click();
      // The modal exposes a heading and a file input.
      const modalHeading = page.getByRole('heading', { name: /Upload trace/i });
      await expect(modalHeading).toBeVisible();
      // Dismiss with Escape rather than clicking the X icon — on Grafana 12.3.x
      // a substring match of `name: 'Close'` also matches the mega-menu's
      // "Close menu" button. Escape works across all versions.
      await page.keyboard.press('Escape');
      await expect(modalHeading).toBeHidden();
    });
  });

  test.describe('search options panel', () => {
    test('summarises the active Search options on the toggle button', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      // The collapsed toggle exposes the active option summary (limit, spans
      // limit, table format, streaming) in its accessible name. Asserting on
      // the summary string is more robust than asserting on the labelled
      // inputs that only render when the panel is expanded.
      await expect(queryRow.getByRole('button', { name: /Search Options.*Limit:.*Table Format:/ })).toBeVisible();
    });
  });
});

// These tests use real trace data continuously emitted by the TNS demo apps
// (db/app/loadgen) via the Jaeger SDK into Tempo. Each navigates to an Explore
// URL with a known query pre-encoded in the panes parameter and asserts on the
// response shape.
test.describe('Query editor with live trace data', () => {
  // Serialize live-data tests so they don't compete for the shared Tempo
  // instance and produce slow responses that look like failures.
  test.describe.configure({ mode: 'serial' });

  // The TraceqlSearch and TraceQL editors only fire /api/ds/query when a
  // syntactically complete query is in the editor and the user clicks Run
  // Query — neither auto-fires from a URL-encoded panes parameter. Service
  // Graph, on the other hand, fires immediately on mount with the configured
  // service-map datasource. Cover that auto-fire path here; the per-mode
  // rendering tests above already prove that the editor for each query type
  // mounts and is interactive.
  test.describe('Service Graph', () => {
    test('returns a 2xx for the default Service Graph query', async ({ page }) => {
      // Service Graph fans out to several Prometheus queries via the
      // configured serviceMap.datasourceUid, each with a refId like
      // `traces_service_graph_request_*`. There is no `results.A` to assert
      // on; instead, wait for the first 2xx /api/ds/query and check the
      // response shape contains any frame from the Service Graph.
      const responsePromise = page.waitForResponse(async (r) => {
        if (!r.url().includes('/api/ds/query') || !r.ok()) {
          return false;
        }
        const b = await r.json().catch(() => null);
        return Object.values(b?.results ?? {}).some((v: any) => Array.isArray(v?.frames));
      });
      await page.goto(exploreUrl('serviceMap'));
      const response = await responsePromise;
      expect(response.ok()).toBe(true);
    });
  });
});
