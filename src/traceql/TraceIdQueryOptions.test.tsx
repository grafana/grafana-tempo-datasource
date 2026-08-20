import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { type TempoQuery } from '../types';

import { TraceIdQueryOptions } from './TraceIdQueryOptions';

type User = ReturnType<typeof userEvent.setup>;

const baseQuery: TempoQuery = {
  refId: 'A',
  queryType: 'traceId',
  query: '9f1b2c3d4e5f6071',
  filters: [],
};

const setup = (overrides: Partial<TempoQuery> = {}) => {
  const onChange = jest.fn();
  const user = userEvent.setup();
  render(<TraceIdQueryOptions query={{ ...baseQuery, ...overrides }} onChange={onChange} />);
  return { onChange, user };
};

const expand = (user: User) => user.click(screen.getByRole('button', { name: /TraceID Query Options/ }));

const lastQuery = (onChange: jest.Mock): TempoQuery => onChange.mock.calls[onChange.mock.calls.length - 1][0];

describe('TraceIdQueryOptions', () => {
  it('reports span pruning as on in the collapsed summary when unset', () => {
    setup();
    expect(screen.getByText('Span Pruning: On')).toBeInTheDocument();
  });

  it('reports span pruning as off in the collapsed summary when explicitly disabled', () => {
    setup({ spanPruning: false });
    expect(screen.getByText('Span Pruning: Off')).toBeInTheDocument();
  });

  it('renders every field once expanded', async () => {
    const { user } = setup();

    expect(screen.queryByText('Group By Attributes')).not.toBeInTheDocument();

    await expand(user);

    expect(screen.getByText('Span Pruning')).toBeInTheDocument();
    expect(screen.getByText('Group By Attributes')).toBeInTheDocument();
    expect(screen.getByText('Min Spans')).toBeInTheDocument();
    expect(screen.getByText('Max Parent Depth')).toBeInTheDocument();
  });

  it('turns span pruning off', async () => {
    const { onChange, user } = setup();
    await expand(user);

    await user.click(screen.getByRole('radio', { name: 'Off' }));

    expect(lastQuery(onChange).spanPruning).toBe(false);
  });

  it('turns span pruning on', async () => {
    const { onChange, user } = setup({ spanPruning: false });
    await expand(user);

    await user.click(screen.getByRole('radio', { name: 'On' }));

    expect(lastQuery(onChange).spanPruning).toBe(true);
  });

  it('disables the sub-parameters when span pruning is off', async () => {
    const { user } = setup({ spanPruning: false });
    await expand(user);

    expect(screen.getByPlaceholderText('db.*,http.method')).toBeDisabled();
    expect(screen.getByPlaceholderText('5')).toBeDisabled();
    expect(screen.getByPlaceholderText('1')).toBeDisabled();
  });

  it('enables the sub-parameters when span pruning is left at its default', async () => {
    const { user } = setup();
    await expand(user);

    expect(screen.getByPlaceholderText('db.*,http.method')).toBeEnabled();
    expect(screen.getByPlaceholderText('5')).toBeEnabled();
    expect(screen.getByPlaceholderText('1')).toBeEnabled();
  });

  it('omits the sub-parameters from the collapsed summary when span pruning is off, since the backend drops them', () => {
    setup({ spanPruning: false, spanPruningGroupBy: 'db.*', spanPruningMinSpans: 10, spanPruningMaxParentDepth: 3 });

    expect(screen.getByText('Span Pruning: Off')).toBeInTheDocument();
    expect(screen.queryByText(/Group By:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Min Spans:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Max Parent Depth:/)).not.toBeInTheDocument();
  });

  it('reports the sub-parameters in the collapsed summary when span pruning is on', () => {
    setup({ spanPruning: true, spanPruningGroupBy: 'db.*', spanPruningMinSpans: 10, spanPruningMaxParentDepth: 3 });

    expect(screen.getByText('Span Pruning: On')).toBeInTheDocument();
    expect(screen.getByText('Group By: db.*')).toBeInTheDocument();
    expect(screen.getByText('Min Spans: 10')).toBeInTheDocument();
    expect(screen.getByText('Max Parent Depth: 3')).toBeInTheDocument();
  });

  it('commits min spans as a number', async () => {
    const { onChange, user } = setup();
    await expand(user);

    await user.type(screen.getByPlaceholderText('5'), '10');
    await user.tab();

    const committed = lastQuery(onChange);
    expect(committed.spanPruningMinSpans).toBe(10);
    expect(typeof committed.spanPruningMinSpans).toBe('number');
  });

  it('commits an undefined min spans when the field is cleared so Tempo owns the default', async () => {
    const { onChange, user } = setup({ spanPruningMinSpans: 10 });
    await expand(user);

    await user.clear(screen.getByPlaceholderText('5'));
    await user.tab();

    const committed = lastQuery(onChange);
    expect(committed).toHaveProperty('spanPruningMinSpans');
    expect(committed.spanPruningMinSpans).toBeUndefined();
  });

  it('commits an undefined group by when the field is cleared so Tempo owns the default', async () => {
    const { onChange, user } = setup({ spanPruningGroupBy: 'db.*' });
    await expand(user);

    await user.clear(screen.getByPlaceholderText('db.*,http.method'));
    await user.tab();

    const committed = lastQuery(onChange);
    expect(committed).toHaveProperty('spanPruningGroupBy');
    expect(committed.spanPruningGroupBy).toBeUndefined();
  });

  it('commits a trimmed group by', async () => {
    const { onChange, user } = setup();
    await expand(user);

    await user.type(screen.getByPlaceholderText('db.*,http.method'), '  db.*  ');
    await user.tab();

    expect(lastQuery(onChange).spanPruningGroupBy).toBe('db.*');
  });

  it('commits an undefined min spans when the value exceeds the safe integer range', async () => {
    const { onChange, user } = setup();
    await expand(user);

    await user.type(screen.getByPlaceholderText('5'), '99999999999999999999');
    await user.tab();

    const committed = lastQuery(onChange);
    expect(committed).toHaveProperty('spanPruningMinSpans');
    expect(committed.spanPruningMinSpans).toBeUndefined();
  });

  it.each([0, -1])('reports a max parent depth of %s verbatim rather than as auto', async (depth) => {
    const { user } = setup({ spanPruningMaxParentDepth: depth });

    expect(screen.getByText(`Max Parent Depth: ${depth}`)).toBeInTheDocument();
    expect(screen.queryByText('Max Parent Depth: auto')).not.toBeInTheDocument();

    await expand(user);

    expect(screen.getByPlaceholderText('1')).toHaveValue(depth);
  });
});
