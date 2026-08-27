/// <reference types="node" />
import { type Page } from '@playwright/test';

/**
 * The reusable Cloud workflow sets GRAFANA_URL. Local runs and PR CI do not,
 * making it a reliable signal that does not depend on Vault secrets resolving.
 */
export const isCloudRun = !!process.env.GRAFANA_URL;

function requireOnCloud(name: string, localDefault: string): string {
  const value = process.env[name]?.trim();
  if (value) {
    return value;
  }
  if (isCloudRun) {
    throw new Error(
      `${name} is not set, but GRAFANA_URL is, so this Cloud run expects it from Vault. ` +
        `Check the repo-secrets paths in .github/workflows/cron.yml; they are relative to ` +
        `ci/repo/grafana/grafana-tempo-datasource/.`
    );
  }
  return localDefault;
}

export const DS_NAME = requireOnCloud('DS_INSTANCE_NAME', 'Tempo');
export const DS_URL = requireOnCloud('DS_INSTANCE_URL', 'http://tempo:3200');

const LOCAL_DS_UID = 'tempo';

/**
 * Resolve the Cloud data source at runtime so its UID cannot drift from a
 * hardcoded test constant.
 */
export async function resolveDataSourceUid(page: Page): Promise<string> {
  const override = process.env.DS_E2E_UID?.trim();
  if (override) {
    return override;
  }
  if (!isCloudRun) {
    return LOCAL_DS_UID;
  }

  const response = await page.request.get('/api/datasources');
  if (!response.ok()) {
    throw new Error(`Could not list data sources on ${process.env.GRAFANA_URL}: HTTP ${response.status()}`);
  }

  const tempoDataSources: Array<{ name: string; uid: string }> = (await response.json()).filter(
    (dataSource: { type: string }) => dataSource.type === 'tempo'
  );
  const managedDataSourceName = `[managed_data_source] - ${DS_NAME}`;
  const exactMatch = tempoDataSources.find(
    (dataSource) => dataSource.name === DS_NAME || dataSource.name === managedDataSourceName
  );
  if (exactMatch) {
    return exactMatch.uid;
  }

  if (tempoDataSources.length === 1) {
    console.warn(
      `DS_INSTANCE_NAME does not match any data source; using the only Tempo data source ` +
        `("${tempoDataSources[0].name}"). Update the Vault secret.`
    );
    return tempoDataSources[0].uid;
  }

  throw new Error(
    `Could not resolve a Tempo data source matching DS_INSTANCE_NAME. Found ` +
      `${tempoDataSources.length} Tempo data source(s): ` +
      `${JSON.stringify(tempoDataSources.map((dataSource) => dataSource.name))}.`
  );
}
