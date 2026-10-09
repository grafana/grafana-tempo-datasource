import { lastValueFrom, of } from 'rxjs';

import { type DataQueryRequest, dateTime } from '@grafana/data';
import { config, DataSourceWithBackend } from '@grafana/runtime';

import { TraceqlSearchScope } from './dataquery';
import { doTempoSearchStreaming } from './streaming';
import { createTempoDatasource } from './test/mocks';
import { initTemplateSrv } from './test/test_utils';
import { type TempoQuery } from './types';

jest.mock('./streaming');

const range = {
  from: dateTime(new Date(2022, 8, 13, 16, 0, 0, 0)),
  to: dateTime(new Date(2022, 8, 13, 16, 15, 0, 0)),
  raw: { from: 'now-15m', to: 'now' },
};

function request(targets: Array<Partial<TempoQuery>>): DataQueryRequest<TempoQuery> {
  return { targets, range, scopedVars: {} } as unknown as DataQueryRequest<TempoQuery>;
}

function serviceNameFilter(name: string) {
  return {
    id: 'service-name',
    scope: TraceqlSearchScope.Resource,
    tag: 'service.name',
    operator: '=',
    value: [name],
    valueType: 'string',
  };
}

const traceqlTargets: Array<Partial<TempoQuery>> = [
  { refId: 'Servers', queryType: 'traceql', query: '{kind=client} > {kind=server}' },
  { refId: 'Clients', queryType: 'traceql', query: '{kind=server} < {kind=client}' },
];

const searchEditorTargets: Array<Partial<TempoQuery>> = [
  { refId: 'A', queryType: 'traceqlSearch', filters: [serviceNameFilter('api')] },
  { refId: 'B', queryType: 'traceqlSearch', filters: [serviceNameFilter('keycloak')] },
];

describe('Tempo data source with multiple search queries', () => {
  const originalLiveEnabled = config.liveEnabled;
  let backendQuery: jest.SpyInstance;

  beforeAll(() => {
    // Search editor filters are interpolated through the global template service
    initTemplateSrv([], {});
  });

  beforeEach(() => {
    backendQuery = jest.spyOn(DataSourceWithBackend.prototype, 'query').mockReturnValue(of({ data: [] }));
    jest.mocked(doTempoSearchStreaming).mockReset().mockReturnValue(of({ data: [] }));
  });

  afterEach(() => {
    backendQuery.mockRestore();
    config.liveEnabled = originalLiveEnabled;
  });

  describe('without streaming', () => {
    beforeEach(() => {
      config.liveEnabled = false;
    });

    it('runs each TraceQL query with its own query text', async () => {
      const ds = createTempoDatasource();
      await lastValueFrom(ds.query(request(traceqlTargets)));

      const sent: TempoQuery[] = backendQuery.mock.calls[0][0].targets;
      expect(sent.map((t) => [t.refId, t.query])).toEqual([
        ['Servers', '{kind=client} > {kind=server}'],
        ['Clients', '{kind=server} < {kind=client}'],
      ]);
    });

    it('runs each search editor query with the query generated from its own filters', async () => {
      const ds = createTempoDatasource();
      await lastValueFrom(ds.query(request(searchEditorTargets)));

      const sent: TempoQuery[] = backendQuery.mock.calls[0][0].targets;
      expect(sent.map((t) => t.refId)).toEqual(['A', 'B']);
      expect(sent[0].query).toContain('"api"');
      expect(sent[1].query).toContain('"keycloak"');
    });
  });

  describe('with streaming', () => {
    beforeEach(() => {
      config.liveEnabled = true;
    });

    it('streams each TraceQL query with its own query text', async () => {
      const ds = createTempoDatasource(undefined, { jsonData: { streamingEnabled: { search: true } } });
      await lastValueFrom(ds.query(request(traceqlTargets)));

      const streamed = jest.mocked(doTempoSearchStreaming).mock.calls.map(([target]) => [target.refId, target.query]);
      expect(streamed).toEqual([
        ['Servers', '{kind=client} > {kind=server}'],
        ['Clients', '{kind=server} < {kind=client}'],
      ]);
    });

    it('streams each search editor query with the query generated from its own filters', async () => {
      const ds = createTempoDatasource(undefined, { jsonData: { streamingEnabled: { search: true } } });
      await lastValueFrom(ds.query(request(searchEditorTargets)));

      const streamed = jest.mocked(doTempoSearchStreaming).mock.calls.map(([target]) => target);
      expect(streamed.map((t) => t.refId)).toEqual(['A', 'B']);
      expect(streamed[0].query).toContain('"api"');
      expect(streamed[1].query).toContain('"keycloak"');
    });

    it('skips queries that are empty', async () => {
      const ds = createTempoDatasource(undefined, { jsonData: { streamingEnabled: { search: true } } });
      await lastValueFrom(
        ds.query(
          request([
            { refId: 'A', queryType: 'traceql', query: '{kind=server}' },
            { refId: 'B', queryType: 'traceql', query: '' },
          ])
        )
      );

      const streamed = jest.mocked(doTempoSearchStreaming).mock.calls.map(([target]) => target.refId);
      expect(streamed).toEqual(['A']);
    });
  });
});
