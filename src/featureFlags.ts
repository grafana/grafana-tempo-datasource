import { MultiProvider, OpenFeature } from '@openfeature/web-sdk';

import { createOpenFeatureLocalStorageProvider, createOpenFeatureOFREPWebProvider } from '@grafana/runtime';

import pluginJson from './plugin.json';

// OpenFeature is a window-global singleton, so evaluations go through a
// plugin-scoped domain to stay isolated from Grafana core and other plugins.
export const OPEN_FEATURE_DOMAIN = pluginJson.id;

// Declared disabled before this file started evaluating it, so lookups
// resolve cleanly even before any rollout targeting is configured.
export const METRICS_STREAMING_DEFAULT_FLAG = 'tempo.datasource.metricsStreamingEnabled';

/**
 * Registers read-only proxies of Grafana's own providers under the plugin's
 * domain. Grafana initializes the underlying providers before plugins load, so
 * the proxies resolve synchronously with no extra flag fetch. Call once at
 * plugin module load.
 */
export function initFeatureFlags(): void {
  // Register when the domain does not already have a provider,
  // so module re-evaluation does not reset OpenFeature state.
  if (OpenFeature.getProvider(OPEN_FEATURE_DOMAIN) === OpenFeature.getProvider()) {
    OpenFeature.setProvider(
      OPEN_FEATURE_DOMAIN,
      new MultiProvider([
        { provider: createOpenFeatureLocalStorageProvider() },
        { provider: createOpenFeatureOFREPWebProvider() },
      ])
    );
  }
}

/**
 * Synchronous read of the Tempo metrics-streaming default flag.
 *
 * This supplies the DEFAULT for jsonData.streamingEnabled.metrics when an
 * instance has never had that field explicitly set - it is not an AND
 * gate. An instance with an explicit jsonData value (on or off) keeps
 * that value regardless of this flag; see datasource.ts.
 */
export function isMetricsStreamingDefaultEnabled(): boolean {
  return OpenFeature.getClient(OPEN_FEATURE_DOMAIN).getBooleanValue(METRICS_STREAMING_DEFAULT_FLAG, false);
}
