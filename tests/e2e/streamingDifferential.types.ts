// Types and constants shared by streamingDifferential.spec.ts, .driver.ts and .helpers.ts.

export type Frame = {
  schema: { name?: string; fields: Array<{ name: string; labels?: Record<string, string> }> };
  data: { values: Array<Array<string | number | null>> };
};

// The columns of a Grafana Live message from the plugin: result, metrics, state, error (one row each).
export type LiveColumns = unknown[][];

export type FinalLiveResult = { state: 'done' | 'error'; result: unknown; error: string };

// A query to run: the datasource, where the window ends, how long it is, and the query fields.
export type QueryRun = { uid: string; end: Date; windowMs: number; query: Record<string, unknown> };

// Both provisioned datasources point at the same Tempo; only `streamingEnabled` differs.
export const STREAMING_UID = 'tempo';
export const NON_STREAMING_UID = 'tempo-no-streaming';

// Closed windows that ended 90 seconds ago keep the result sets stable while the TNS apps keep emitting data. They are
// short so they stay clear of Tempo's block retention, and end on a whole second because the HTTP path truncates the
// time range to seconds while streaming doesn't. The metrics window contains the search window.
export const SEARCH_WINDOW_MS = 30 * 1000;
export const METRICS_WINDOW_MS = 10 * 60 * 1000;

// The limit is above the number of traces in the window: with fewer matches than the limit Tempo returns all of them,
// otherwise it returns an arbitrary subset that can differ between two runs.
export const SEARCH_LIMIT = 1000;
export const SEARCH_QUERY = { query: '{}', limit: SEARCH_LIMIT, tableType: 'traces' };
export const METRICS_QUERY = '{} | rate() by (resource.service.name)';

// How long a single wait may take: a response, a click, the end of a stream.
export const ACTION_TIMEOUT_MS = 30_000;
