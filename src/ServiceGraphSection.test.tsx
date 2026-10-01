import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { type DataSourceApi } from '@grafana/data';
import { type DataSourceSrv, setDataSourceSrv } from '@grafana/runtime';

import { ServiceGraphSection } from './ServiceGraphSection';
import { type AdHocVariableFilter } from './_importedDependencies/components/AdHocFilter/types';
import { type TempoQuery } from './types';

// The real filter UI is a set of dropdown segments. This stub calls the same callbacks directly so
// the test covers how ServiceGraphSection stores the edited query rather than the segment rendering.
jest.mock('./_importedDependencies/components/AdHocFilter/AdHocFilter', () => ({
  AdHocFilter: ({
    filters,
    addFilter,
    removeFilter,
    changeFilter,
  }: {
    filters: AdHocVariableFilter[];
    addFilter: (filter: AdHocVariableFilter) => void;
    removeFilter: (index: number) => void;
    changeFilter: (index: number, filter: AdHocVariableFilter) => void;
  }) => (
    <div>
      <div data-testid="filters">{JSON.stringify(filters.map((f) => `${f.key}${f.operator}${f.value}`))}</div>
      <button onClick={() => addFilter({ key: 'server', operator: '=', value: 'db', condition: '' })}>add</button>
      <button onClick={() => removeFilter(0)}>remove</button>
      <button onClick={() => changeFilter(0, { key: 'client', operator: '=', value: 'web', condition: '' })}>
        change
      </button>
    </div>
  ),
}));

const prometheusMock = () =>
  ({
    getTagKeys: jest.fn().mockResolvedValue([{ text: 'traces_service_graph_request_total' }]),
  }) as unknown as DataSourceApi;

const dataSourceSrvWithPrometheus = (promMock: DataSourceApi) =>
  ({
    async get(uid: string) {
      if (uid === 'prom') {
        return promMock;
      }
      throw new Error('unexpected uid');
    },
  }) as unknown as DataSourceSrv;

async function renderSection(serviceMapQuery: TempoQuery['serviceMapQuery']) {
  setDataSourceSrv(dataSourceSrvWithPrometheus(prometheusMock()));
  const onChange = jest.fn();
  const query = { refId: 'A', queryType: 'serviceMap', serviceMapQuery } as TempoQuery;
  render(<ServiceGraphSection graphDatasourceUid="prom" query={query} onChange={onChange} />);
  await screen.findByText('add');
  return onChange;
}

describe('ServiceGraphSection', () => {
  it('shows the filters of a single service map query and keeps it a string when edited', async () => {
    const onChange = await renderSection('{client="app"}');

    expect(screen.getByTestId('filters')).toHaveTextContent('client=app');

    await userEvent.click(screen.getByText('add'));
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ serviceMapQuery: '{client="app",server="db"}' })
    );
  });

  it('keeps the remaining expressions of a multi expression query when a filter is added', async () => {
    const onChange = await renderSection(['{client="app"}', '{server="app"}']);

    await userEvent.click(screen.getByText('add'));
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ serviceMapQuery: ['{client="app",server="db"}', '{server="app"}'] })
    );
  });

  it('keeps the remaining expressions of a multi expression query when a filter is changed', async () => {
    const onChange = await renderSection(['{client="app"}', '{server="app"}']);

    await userEvent.click(screen.getByText('change'));
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ serviceMapQuery: ['{client="web"}', '{server="app"}'] })
    );
  });

  it('keeps the remaining expressions of a multi expression query when a filter is removed', async () => {
    const onChange = await renderSection(['{client="app"}', '{server="app"}']);

    await userEvent.click(screen.getByText('remove'));
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ serviceMapQuery: ['{}', '{server="app"}'] }));
  });
});
