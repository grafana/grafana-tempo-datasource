import { MultiProvider, OpenFeature } from "@openfeature/web-sdk";

import { createOpenFeatureLocalStorageProvider, createOpenFeatureOFREPWebProvider } from "@grafana/runtime";

import pluginJson from "./plugin.json";

const OPEN_FEATURE_DOMAIN = pluginJson.id;

const STREAMING_DISABLED_FLAG = "datasources.tempo.noStreaming";


export function initFeatureFlags():void {
    if (OpenFeature.getProvider(OPEN_FEATURE_DOMAIN) === OpenFeature.getProvider()) {
    OpenFeature.setProvider(
        OPEN_FEATURE_DOMAIN,
        new MultiProvider([
        { provider: createOpenFeatureLocalStorageProvider() },
        { provider: createOpenFeatureOFREPWebProvider() },
        ]),
    );
    }
}

export function isStreamingDisabled():boolean {
    return OpenFeature.getClient(OPEN_FEATURE_DOMAIN).getBooleanValue(STREAMING_DISABLED_FLAG, false)
}