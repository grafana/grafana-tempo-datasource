import { expect, test } from '@grafana/plugin-e2e';
import { type Locator, type Page } from '@playwright/test';

const PLUGIN_TYPE = 'tempo';

// Grafana 12.4+ replaced the legacy `DataSourceHttpSettings` (with the "HTTP"
// heading and `data-testid Datasource HTTP settings url` id) with the new
// connection-config layout that exposes a `Data source connection URL` textbox
// under a `Connection` heading. Match both styles so the test stays robust
// across Grafana versions.
function getDataSourceHttpUrlInput(page: Page): Locator {
  return page
    .getByRole('textbox', { name: 'Data source connection URL' })
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
  test(
    'smoke: should render config editor',
    { tag: '@plugins' },
    async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      await expect(getConnectionHeading(page)).toBeVisible({ timeout: 30_000 });
      await expect(getDataSourceHttpUrlInput(page)).toBeVisible();
      await expect(page.locator('#basic-settings-name')).toBeVisible();
      // Tempo-specific sections that always render at the top level of the
      // config editor (Service Graph / Span bar moved inside `Additional
      // settings` and are collapsed by default in Grafana 12.4+).
      await expect(page.getByRole('heading', { name: 'Streaming', exact: true })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Trace to logs', exact: true })).toBeVisible();
    }
  );

  test(
    '"Save & test" should be successful when configuration is valid',
    { tag: '@plugins' },
    async ({ createDataSourceConfigPage, readProvisionedDataSource, page }) => {
      const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
      const configPage = await createDataSourceConfigPage({ type: ds.type });
      await expect(getConnectionHeading(page)).toBeVisible({ timeout: 30_000 });
      await getDataSourceHttpUrlInput(page).fill(ds.url ?? 'http://tempo:3200');
      await expect(configPage.saveAndTest()).toBeOK();
    }
  );
});
