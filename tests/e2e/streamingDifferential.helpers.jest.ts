import {
  finalLiveResult,
  framesFromStreamedResult,
  isQueryRequest,
  parseLiveMessages,
  secondsAgo,
  summarize,
  traceIdsFromFrames,
  traceIdsFromStreamedTraces,
} from './streamingDifferential.helpers';
import { type Frame } from './streamingDifferential.types';

// A Grafana Live message as the browser receives it: the columns result, metrics, state and error, one row each.
const liveMessage = (state: string, result: unknown, error = '') =>
  JSON.stringify({
    push: {
      channel: 'ds/tempo/search/abc',
      pub: { data: { schema: {}, data: { values: [[result], [null], [state], [error]] } } },
    },
  });

const frame = (name: string, labels: Record<string, string>, values: Array<number | null>): Frame => ({
  schema: { name, fields: [{ name: 'time' }, { name: 'value', labels }] },
  data: { values: [values.map((_, i) => i * 1000), values] },
});

describe('parseLiveMessages', () => {
  it('returns the columns of a data message', () => {
    expect(parseLiveMessages(liveMessage('streaming', [{ traceID: 'a' }]))).toEqual([
      [[[{ traceID: 'a' }]], [null], ['streaming'], ['']],
    ]);
  });

  it('returns every message of a frame that holds several, separated by newlines', () => {
    const payload = [liveMessage('streaming', 1), liveMessage('done', 2)].join('\n');
    const messages = parseLiveMessages(payload);
    expect(messages).toHaveLength(2);
    expect(messages.map((m) => m[2][0])).toEqual(['streaming', 'done']);
  });

  it('skips connect replies, pings, empty lines and text that is not JSON', () => {
    const payload = ['{"id":1,"connect":{"client":"x"}}', '{}', '', '   ', 'not json', liveMessage('done', 1), ''].join(
      '\n'
    );
    expect(parseLiveMessages(payload)).toHaveLength(1);
  });

  it('skips pushes that carry no data columns', () => {
    expect(parseLiveMessages('{"push":{"channel":"x","pub":{"data":{}}}}')).toEqual([]);
  });
});

describe('finalLiveResult', () => {
  it('is undefined while the stream is running', () => {
    expect(finalLiveResult(parseLiveMessages(liveMessage('streaming', [])))).toBeUndefined();
    expect(finalLiveResult([])).toBeUndefined();
  });

  it('returns the last message that is done', () => {
    const messages = parseLiveMessages(
      [liveMessage('streaming', 'a'), liveMessage('done', 'b'), liveMessage('done', 'c')].join('\n')
    );
    expect(finalLiveResult(messages)).toEqual({ state: 'done', result: 'c', error: '' });
  });

  it('is not fooled by a streaming message after done', () => {
    const messages = parseLiveMessages([liveMessage('done', 'b'), liveMessage('streaming', 'late')].join('\n'));
    expect(finalLiveResult(messages)?.result).toBe('b');
  });

  it('reports an error with its text', () => {
    const messages = parseLiveMessages(liveMessage('error', null, 'bad query'));
    expect(finalLiveResult(messages)).toEqual({ state: 'error', result: null, error: 'bad query' });
  });

  it('keeps a null result, which is what a search without traces streams', () => {
    expect(finalLiveResult(parseLiveMessages(liveMessage('done', null)))).toEqual({
      state: 'done',
      result: null,
      error: '',
    });
  });
});

describe('traceIdsFromFrames', () => {
  const traces = (ids: string[]): Frame => ({
    schema: { fields: [{ name: 'traceID' }, { name: 'traceService' }] },
    data: { values: [ids, ids.map(() => 'svc')] },
  });

  it('collects the traceID column of every frame', () => {
    expect(traceIdsFromFrames([traces(['a', 'b']), traces(['c'])])).toEqual(['a', 'b', 'c']);
  });

  it('ignores frames without a traceID column and empty results', () => {
    expect(traceIdsFromFrames([frame('series', {}, [1, 2])])).toEqual([]);
    expect(traceIdsFromFrames([])).toEqual([]);
  });
});

describe('traceIdsFromStreamedTraces', () => {
  it('reads the IDs of the streamed traces', () => {
    expect(traceIdsFromStreamedTraces([{ traceID: 'a', rootServiceName: 'x' }, { traceID: 'b' }])).toEqual(['a', 'b']);
  });

  it('treats null as no traces', () => {
    expect(traceIdsFromStreamedTraces(null)).toEqual([]);
    expect(traceIdsFromStreamedTraces(undefined)).toEqual([]);
  });

  it('fails on a result it does not understand instead of comparing nothing', () => {
    expect(() => traceIdsFromStreamedTraces({ traces: [] })).toThrow('not a list');
    expect(() => traceIdsFromStreamedTraces([{ id: 'a' }])).toThrow('no traceID');
  });
});

describe('framesFromStreamedResult', () => {
  it('returns the streamed frames, or none for null', () => {
    const frames = [frame('a', {}, [1])];
    expect(framesFromStreamedResult(frames)).toBe(frames);
    expect(framesFromStreamedResult(null)).toEqual([]);
  });

  it('fails on a result it does not understand', () => {
    expect(() => framesFromStreamedResult('x')).toThrow('not a list');
  });
});

describe('summarize', () => {
  it('describes each series by name, labels and values, sorted', () => {
    const lines = summarize([frame('db', { service: 'db' }, [1, 2]), frame('app', { service: 'app' }, [3])]);
    expect(lines).toEqual(['app|{"service":"app"}|[3]', 'db|{"service":"db"}|[1,2]']);
  });

  it('does not depend on the order of the frames', () => {
    const a = frame('a', { s: 'a' }, [1]);
    const b = frame('b', { s: 'b' }, [2]);
    expect(summarize([a, b])).toEqual(summarize([b, a]));
  });

  it('ignores floating point noise beyond 9 significant digits', () => {
    expect(summarize([frame('x', {}, [0.30000000000000004])])).toEqual(summarize([frame('x', {}, [0.3])]));
    expect(summarize([frame('x', {}, [1.2178])])).not.toEqual(summarize([frame('x', {}, [1.225])]));
  });

  it('keeps null values and tells them apart from zero', () => {
    expect(summarize([frame('x', {}, [null])])).not.toEqual(summarize([frame('x', {}, [0])]));
  });

  it('leaves exemplar frames out, because they are sampled', () => {
    expect(summarize([frame('exemplar', {}, [1]), frame('x', {}, [1])])).toEqual(summarize([frame('x', {}, [1])]));
  });

  it('tells series with different labels apart', () => {
    expect(summarize([frame('x', { s: 'a' }, [1])])).not.toEqual(summarize([frame('x', { s: 'b' }, [1])]));
  });

  it('keeps a missing series visible', () => {
    expect(summarize([frame('a', {}, [1]), frame('b', {}, [1])])).not.toEqual(summarize([frame('a', {}, [1])]));
  });
});

describe('secondsAgo', () => {
  it('ends on a whole second, at or before the requested time', () => {
    const before = Date.now();
    const d = secondsAgo(90);
    expect(d.getTime() % 1000).toBe(0);
    expect(d.getTime()).toBeLessThanOrEqual(before - 90_000);
    expect(d.getTime()).toBeGreaterThan(before - 91_000 - 1000);
  });
});

describe('isQueryRequest', () => {
  const body = (...queries: unknown[]) => ({ queries });

  it('accepts a request that holds the datasource and the query', () => {
    expect(isQueryRequest(body({ datasource: { uid: 'a' }, query: '{}' }), 'a', '{}')).toBe(true);
  });

  it('finds the query among others sent in the same request', () => {
    const queries = [
      { datasource: { uid: 'b' }, query: '{}' },
      { datasource: { uid: 'a' }, query: '{}' },
    ];
    expect(isQueryRequest(body(...queries), 'a', '{}')).toBe(true);
  });

  it('rejects another datasource or another query', () => {
    expect(isQueryRequest(body({ datasource: { uid: 'b' }, query: '{}' }), 'a', '{}')).toBe(false);
    expect(isQueryRequest(body({ datasource: { uid: 'a' }, query: '{ }' }), 'a', '{}')).toBe(false);
  });

  it('rejects bodies that are not a query request', () => {
    expect(isQueryRequest(null, 'a', '{}')).toBe(false);
    expect(isQueryRequest({}, 'a', '{}')).toBe(false);
    expect(isQueryRequest(body(null), 'a', '{}')).toBe(false);
  });
});
