import { render } from '@testing-library/react';

import { type DataSourcePluginOptionsEditorProps } from '@grafana/data';

import { type TraceqlFilter } from '../dataquery';
import { type TempoDatasource } from '../datasource';
import { type TempoJsonData } from '../types';

import { TraceQLSearchTags } from './TraceQLSearchTags';

let capturedUpdateFilter: ((s: TraceqlFilter) => void) | undefined;

jest.mock('../SearchTraceQLEditor/TagsInput', () => ({
  __esModule: true,
  default: (props: { updateFilter: (s: TraceqlFilter) => void }) => {
    capturedUpdateFilter = props.updateFilter;
    return null;
  },
}));

describe('TraceQLSearchTags', () => {
  it('does not mutate the existing filters array when adding a new filter', () => {
    const existingFilter: TraceqlFilter = { id: 'service-name', tag: 'service.name', operator: '=' };
    const originalFilters = [existingFilter];

    const onOptionsChange = jest.fn();
    const options = {
      jsonData: { search: { filters: originalFilters } },
    } as unknown as DataSourcePluginOptionsEditorProps<TempoJsonData>['options'];

    const datasource = {
      languageProvider: { start: jest.fn().mockResolvedValue(undefined) },
    } as unknown as TempoDatasource;

    render(<TraceQLSearchTags options={options} onOptionsChange={onOptionsChange} datasource={datasource} />);

    const newFilter: TraceqlFilter = { id: 'span-name', tag: 'name', operator: '=' };
    capturedUpdateFilter?.(newFilter);

    // The array TraceQLSearchTags was given as a prop must be left untouched:
    // mutating props.options.jsonData.search.filters in place would silently
    // corrupt the caller's own state (see grafana/grafana-tempo-datasource#181).
    expect(originalFilters).toEqual([existingFilter]);

    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        jsonData: expect.objectContaining({
          search: expect.objectContaining({ filters: [existingFilter, newFilter] }),
        }),
      })
    );
  });
});
