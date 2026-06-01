import { expect, test } from '@grafana/plugin-e2e';
import { type Locator, type Page } from '@playwright/test';

import { type TempoJsonData } from '../../src/types';

const PLUGIN_TYPE = 'tempo';
const PROVISIONED_FILE = 'datasources.yml';

// In CI/Cloud the data source URL is provisioned from Vault and exposed via
// DS_INSTANCE_URL. Locally docker-compose names the backend `tempo` and the
// provisioned datasources.yml uses http://tempo:3200.
const DS_URL = process.env.DS_INSTANCE_URL || 'http://tempo:3200';

// Grafana 12.4+ replaced the legacy `DataSourceHttpSettings` (with the "HTTP"
// heading and `data-testid Datasource HTTP settings url` id) with the new
// connection-config layout that exposes a `Data source connection URL` textbox
// under a `Connection` heading. Grafana 13 then migrated multiple UI surfaces
// from aria-label to data-testid (grafana/grafana#121784). Match all shapes so
// the test stays robust across versions.
function getDataSourceConnectionUrlInput(page: Page): Locator {
  return page
    .getByTestId('data-testid Data source connection URL')
    .or(page.getByRole('textbox', { name: 'Data source connection URL' }))
    .or(page.getByTestId('data-testid Datasource HTTP settings url'))
    .or(page.getByTestId('Datasource HTTP settings url'))
    .or(page.getByRole('textbox', { name: 'URL' }))
    .or(page.getByPlaceholder('http://tempo:3200'));
}

function getConnectionHeading(page: Page): Locator {
  return page
    .getByRole('heading', { name: 'Connection', exact: true })
    .or(page.getByRole('heading', { name: 'HTTP', exact: true }));
}

test.describe('Config editor', () => {
  test.describe('rendering', () => {
    test('smoke: should render config editor', { tag: '@plugins' }, async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      // Grafana <=13.0: "Type: Tempo" subtitle in the page header.
      // Grafana >=13.1: subtitle removed (grafana/grafana#123966).
      // Fall back to the Connection heading so this also serves as the
      // page-load wait on builds where the type label is gone.
      await expect(
        page
          .getByText('Type: Tempo', { exact: true })
          .or(page.getByText(/^Type\s*Tempo$/))
          .or(getConnectionHeading(page))
          .first()
      ).toBeVisible({ timeout: 30_000 });
      await expect(getConnectionHeading(page)).toBeVisible();
      await expect(getDataSourceConnectionUrlInput(page)).toBeVisible();
      // Grafana >=13.1 replaced the #basic-settings-name input with an inline
      // editable heading. Match both shapes so the test works across versions.
      // .first() avoids a strict-mode violation on builds that render both.
      await expect(
        page
          .locator('#basic-settings-name')
          .or(page.getByRole('button', { name: 'Edit title' }))
          .first()
      ).toBeVisible();
    });

    test('should render Authentication section', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const heading = page.getByRole('heading', { name: 'Authentication', exact: true });
      await heading.scrollIntoViewIfNeeded();
      await expect(heading).toBeVisible();
      // Auth method combobox is rendered by @grafana/plugin-ui
      await expect(page.getByRole('combobox', { name: 'Authentication method' })).toBeVisible();
    });

    test('should render Streaming section', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const heading = page.getByRole('heading', { name: 'Streaming', exact: true });
      await heading.scrollIntoViewIfNeeded();
      await expect(heading).toBeVisible();
      // Tempo exposes two streaming toggles: Search queries and Metrics queries.
      await expect(page.getByRole('switch', { name: 'Search queries' })).toBeVisible();
      await expect(page.getByRole('switch', { name: 'Metrics queries' })).toBeVisible();
    });

    test('should render Trace to logs section', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const heading = page.getByRole('heading', { name: 'Trace to logs', exact: true });
      await heading.scrollIntoViewIfNeeded();
      await expect(heading).toBeVisible();
    });

    test('should render Trace to metrics section', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const heading = page.getByRole('heading', { name: 'Trace to metrics', exact: true });
      await heading.scrollIntoViewIfNeeded();
      await expect(heading).toBeVisible();
    });

    test('should render Trace to profiles section', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const heading = page.getByRole('heading', { name: 'Trace to profiles', exact: true });
      await heading.scrollIntoViewIfNeeded();
      await expect(heading).toBeVisible();
    });

    test('should render Additional settings section', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const heading = page.getByRole('heading', { name: 'Additional settings', exact: true });
      await heading.scrollIntoViewIfNeeded();
      await expect(heading).toBeVisible();
    });
  });

  test.describe('provisioned datasource', () => {
    test('should load provisioned URL', async ({ readProvisionedDataSource, gotoDataSourceConfigPage, page }) => {
      const ds = await readProvisionedDataSource<TempoJsonData>({ fileName: PROVISIONED_FILE });
      await gotoDataSourceConfigPage(ds.uid);

      await getConnectionHeading(page).scrollIntoViewIfNeeded();
      await expect(getDataSourceConnectionUrlInput(page)).toHaveValue(DS_URL);
    });

    test('should load provisioned Streaming settings', async ({
      readProvisionedDataSource,
      gotoDataSourceConfigPage,
      page,
    }) => {
      const ds = await readProvisionedDataSource<TempoJsonData>({ fileName: PROVISIONED_FILE });
      await gotoDataSourceConfigPage(ds.uid);

      await page.getByRole('heading', { name: 'Streaming', exact: true }).scrollIntoViewIfNeeded();

      // The provisioned datasource enables search streaming.
      const expectedSearch = ds.jsonData?.streamingEnabled?.search === true;
      const searchSwitch = page.getByRole('switch', { name: 'Search queries' });
      if (expectedSearch) {
        await expect(searchSwitch).toBeChecked();
      } else {
        await expect(searchSwitch).not.toBeChecked();
      }
    });
  });

  test.describe('save & test', () => {
    test('should pass health check for provisioned datasource', async ({
      readProvisionedDataSource,
      gotoDataSourceConfigPage,
      page,
    }) => {
      const ds = await readProvisionedDataSource({ fileName: PROVISIONED_FILE });
      const configPage = await gotoDataSourceConfigPage(ds.uid);

      // Match both `Save & test` (editable: true) and `Test` (editable: false).
      // A bare role-based click is more robust than configPage.saveAndTest(),
      // which times out for non-editable provisioned datasources.
      await page.getByRole('button', { name: /^(Save & test|Test)$/ }).click();
      await expect(configPage).toHaveAlert('success');
    });

    test('should show error alert when health check fails', async ({ createDataSourceConfigPage, page }) => {
      const configPage = await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      await expect(getConnectionHeading(page)).toBeVisible({ timeout: 30_000 });
      // `localhost` from inside the Grafana container never resolves to the
      // Tempo service running in a sibling container.
      await getDataSourceConnectionUrlInput(page).fill('http://localhost:3200');
      await page.getByRole('button', { name: /^(Save & test|Test)$/ }).click();
      await expect(configPage).toHaveAlert('error');
    });

    test('should show error alert when backend is unreachable', async ({ createDataSourceConfigPage, page }) => {
      const configPage = await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      await expect(getConnectionHeading(page)).toBeVisible({ timeout: 30_000 });
      // Point at a port nothing is listening on (uses the Cloud host where present).
      const url = DS_URL.replace(/:(\d+)$/, ':13200');
      await getDataSourceConnectionUrlInput(page).fill(url);
      await page.getByRole('button', { name: /^(Save & test|Test)$/ }).click();
      await expect(configPage).toHaveAlert('error');
    });
  });
});
