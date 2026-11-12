import { NEVER } from 'rxjs';

import { type DataQueryRequest, type DataSourceInstanceSettings, dateTime, LiveChannelScope } from '@grafana/data';
import { getGrafanaLiveSrv } from '@grafana/runtime';

import { type TempoDatasource } from './datasource';
import { doTempoMetricsStreaming, doTempoSearchStreaming } from './streaming';
import { type TempoJsonData, type TempoQuery } from './types';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getGrafanaLiveSrv: jest.fn(),
}));

const ds = { uid: 'tempo-uid' } as TempoDatasource;
const options = {
  range: { from: dateTime(0), to: dateTime(1000), raw: { from: 'now-1h', to: 'now' } },
} as DataQueryRequest<TempoQuery>;
const query = { refId: 'A', query: '{}' } as TempoQuery;

describe('streaming Live channel address', () => {
  const getStream = jest.fn();

  beforeEach(() => {
    getStream.mockReset().mockReturnValue(NEVER);
    jest.mocked(getGrafanaLiveSrv).mockReturnValue({ getStream } as unknown as ReturnType<typeof getGrafanaLiveSrv>);
  });

  // Grafana 12.3 reads `namespace` and later versions read `stream`, so both must name the datasource.
  it.each([
    ['search', () => doTempoSearchStreaming(query, ds, options, {} as DataSourceInstanceSettings<TempoJsonData>)],
    ['metrics', () => doTempoMetricsStreaming(query, ds, options)],
  ])('%s names the datasource in stream and namespace', (_, start) => {
    start();

    expect(getStream).toHaveBeenCalledWith(
      expect.objectContaining({ scope: LiveChannelScope.DataSource, stream: ds.uid, namespace: ds.uid })
    );
  });
});
