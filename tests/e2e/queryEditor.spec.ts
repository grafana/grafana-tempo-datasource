import { expect, test } from '@grafana/plugin-e2e';

test.describe('Query editor', () => {
  test(
    'smoke: renders TraceQL query type tabs',
    { tag: '@plugins' },
    async ({ explorePage, page, readProvisionedDataSource }) => {
      const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
      await explorePage.datasource.set(ds.name);

      // The Tempo query editor exposes a RadioButtonGroup ("Query type") with
      // Search, TraceQL, Service Graph, Search by Trace ID. Assert that the tabs
      // load in Explore so we know the editor mounted without crashing.
      await expect(page.getByRole('radio', { name: 'Search' }).first()).toBeVisible({ timeout: 30_000 });
      await expect(page.getByRole('radio', { name: 'TraceQL' })).toBeVisible();
      await expect(page.getByRole('radio', { name: 'Service Graph' })).toBeVisible();
    }
  );
});
