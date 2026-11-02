import { OFREPWebProvider } from "@openfeature/ofrep-web-provider";
import { OpenFeature } from "@openfeature/web-sdk";

import { config } from "@grafana/runtime";

import pluginJson from "./plugin.json";

const OPEN_FEATURE_DOMAIN = pluginJson.id;

const STREAMING_DISABLED_FLAG = "datasources.tempo.noStreaming";

// this feature only works properly in newer grafana versions,
// so if we run the plugin in an older grafana version,
// we return a hardcoded result.
//
function isModernGrafana(): boolean {
  // we accept v13.0.0 or newer
  const v = config.buildInfo.version;
  // we do not want to import additional packages for this, so we will do it in a simple way
  const major = Number(v.split('.')[0])
  // value is NaN if the conversion failed
  if (Number.isNaN(major)) {
    return false;
  }

  return major >= 13;
}

export function initFeatureFlags(): void {
  if (!isModernGrafana()) {
    return;
  }

  if (OpenFeature.getProvider(OPEN_FEATURE_DOMAIN) === OpenFeature.getProvider()) {
    // the current codebase imports an older version of grafana-runtime,
    // so we cannot use the newer approach, we go with the older one.
    OpenFeature.setProvider(
      OPEN_FEATURE_DOMAIN,
      new OFREPWebProvider({
        baseUrl: `${config.appSubUrl || ""}/apis/features.grafana.app/v0alpha1/namespaces/${config.namespace}`,
        disableVisibilityRefresh: true,
        cacheMode: "disabled",
        timeoutMs: 10_000,
      }),
      {
        targetingKey: config.namespace,
        ...config.openFeatureContext,
      }
    )
  }
}

export function isStreamingDisabled(): boolean {
  if (!isModernGrafana()) {
      return false;
  }
  return OpenFeature.getClient(OPEN_FEATURE_DOMAIN).getBooleanValue(STREAMING_DISABLED_FLAG, false)
}
