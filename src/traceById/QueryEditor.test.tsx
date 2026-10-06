import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { type QueryEditorProps } from '@grafana/data';

import { type TempoDatasource } from '../datasource';
import { type MyDataSourceOptions, type TempoQuery } from '../types';

import { QueryEditor } from './QueryEditor';

jest.mock('./TraceIdInput', () => ({
  TraceIdInput: ({ query, onChange }: { query: TempoQuery; onChange: (value: TempoQuery) => void }) => (
    <button data-testid="trace-id-input" onClick={() => onChange({ ...query, query: 'abc123' })} type="button" />
  ),
}));

jest.mock('./TraceIdOptions', () => ({
  TraceIdOptions: ({ query, onChange }: { query: TempoQuery; onChange: (value: TempoQuery) => void }) => (
    <button data-testid="trace-id-options" onClick={() => onChange({ ...query, spanPruning: true })} type="button" />
  ),
}));

type Props = QueryEditorProps<TempoDatasource, TempoQuery, MyDataSourceOptions>;

describe('TraceIdQueryEditor', () => {
  function renderQueryEditor(overrides: Partial<Props> = {}) {
    const onChange = jest.fn();
    const props = {
      datasource: {} as TempoDatasource,
      query: { refId: 'A', queryType: 'traceId' } as TempoQuery,
      onChange,
      onRunQuery: jest.fn(),
      ...overrides,
    } as Props;

    return { ...render(<QueryEditor {...props} />), onChange, props };
  }

  it('renders without crashing and mounts the trace ID input and options', () => {
    renderQueryEditor();

    expect(screen.getByTestId('trace-id-input')).toBeInTheDocument();
    expect(screen.getByTestId('trace-id-options')).toBeInTheDocument();
  });

  it('propagates onChange from the trace ID input', async () => {
    const user = userEvent.setup();
    const { onChange } = renderQueryEditor();

    await user.click(screen.getByTestId('trace-id-input'));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ query: 'abc123' }));
  });

  it('propagates onChange from the trace ID options', async () => {
    const user = userEvent.setup();
    const { onChange } = renderQueryEditor();

    await user.click(screen.getByTestId('trace-id-options'));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ spanPruning: true }));
  });
});
