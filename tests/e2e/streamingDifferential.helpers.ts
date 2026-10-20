// Parsing and comparison for streamingDifferential.spec.ts. Nothing here touches the browser, so it is unit tested with
// Jest (streamingDifferential.helpers.jest.ts).

export type Frame = {
  schema: { name?: string; fields: Array<{ name: string; labels?: Record<string, string> }> };
  data: { values: Array<Array<string | number | null>> };
};

// The columns of a Grafana Live message from the plugin: result, metrics, state, error (one row each).
export type LiveColumns = unknown[][];

export type FinalLiveResult = { state: 'done' | 'error'; result: unknown; error: string };

export function secondsAgo(seconds: number): Date {
  return new Date(Math.floor((Date.now() - seconds * 1000) / 1000) * 1000);
}

// A websocket frame can hold several messages separated by newlines. Keeps the columns of the data messages and skips
// everything else (connect, ping, subscribe replies, empty lines).
export function parseLiveMessages(payload: string): LiveColumns[] {
  const out: LiveColumns[] = [];
  for (const line of payload.split('\n')) {
    if (line.trim() === '') {
      continue;
    }
    let message;
    try {
      message = JSON.parse(line);
    } catch {
      continue;
    }
    const values = message?.push?.pub?.data?.data?.values;
    if (Array.isArray(values)) {
      out.push(values);
    }
  }
  return out;
}

// The last message in state "done" or "error", which carries the final result. Undefined while the stream is running.
export function finalLiveResult(messages: LiveColumns[]): FinalLiveResult | undefined {
  const finals = messages.filter((m) => m[2]?.[0] === 'done' || m[2]?.[0] === 'error');
  const last = finals[finals.length - 1];
  if (!last) {
    return undefined;
  }
  return { state: last[2][0] === 'error' ? 'error' : 'done', result: last[0]?.[0], error: String(last[3]?.[0] ?? '') };
}

// The trace IDs of the non-streaming search response (the `traceID` column of its frames).
export function traceIdsFromFrames(frames: Frame[]): string[] {
  return frames.flatMap((frame) => {
    const index = frame.schema.fields.findIndex((f) => f.name === 'traceID');
    return index >= 0 ? frame.data.values[index].map(String) : [];
  });
}

// The trace IDs of the streamed search result. A search without traces is streamed as null.
export function traceIdsFromStreamedTraces(result: unknown): string[] {
  if (result === null || result === undefined) {
    return [];
  }
  if (!Array.isArray(result)) {
    throw new Error(`streamed search result is not a list: ${JSON.stringify(result).slice(0, 200)}`);
  }
  return result.map((trace) => {
    if (typeof trace?.traceID !== 'string') {
      throw new Error(`streamed trace has no traceID: ${JSON.stringify(trace).slice(0, 200)}`);
    }
    return trace.traceID;
  });
}

// The frames of a streamed metrics result. A result without series is streamed as null.
export function framesFromStreamedResult(result: unknown): Frame[] {
  if (result === null || result === undefined) {
    return [];
  }
  if (!Array.isArray(result)) {
    throw new Error(`streamed metrics result is not a list: ${JSON.stringify(result).slice(0, 200)}`);
  }
  return result;
}

// One line per series: name, labels and values (to 9 significant digits, so floating point noise does not count). Time
// is left out because instant frames use the current time.
export function summarize(frames: Frame[]): string[] {
  return frames
    .filter((frame) => frame.schema.name !== 'exemplar') // exemplars are sampled, so they differ between runs
    .map((frame) => {
      const valueIndex = frame.schema.fields.length - 1;
      const field = frame.schema.fields[valueIndex];
      const values = frame.data.values[valueIndex].map((v) => (typeof v === 'number' ? Number(v.toPrecision(9)) : v));
      return `${frame.schema.name}|${JSON.stringify(field.labels)}|${JSON.stringify(values)}`;
    })
    .sort();
}
