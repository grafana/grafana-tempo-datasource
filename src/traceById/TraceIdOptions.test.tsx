import * as React from 'react';

import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { type TempoQuery } from '../types';

import { TraceIdOptions } from './TraceIdOptions';

describe('TraceIdOptions', () => {
  function baseQuery(overrides: Partial<TempoQuery> = {}): TempoQuery {
    return { refId: 'A', queryType: 'traceId', ...overrides } as TempoQuery;
  }

  function getAncestorDepthInput() {
    const inputs = screen.getAllByTestId('autosize-input');
    return inputs[inputs.length - 1];
  }

  function getMatchDepthInput() {
    const inputs = screen.getAllByTestId('autosize-input');
    return inputs[inputs.length - 2];
  }

  // Both groups render an On/Off RadioButtonGroup with identical option labels (Span Pruning's
  // On/Off, and Filter's Keep Hierarchy), so disambiguate by DOM order: Keep Hierarchy's is second.
  function getKeepHierarchyButton(label: 'On' | 'Off') {
    return screen.getAllByRole('radio', { name: label })[1];
  }

  it('disables Keep Hierarchy and Match Depth when there is no valid filter', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery()} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(getKeepHierarchyButton('On')).toBeDisabled();
    expect(getKeepHierarchyButton('Off')).toBeDisabled();
    expect(getMatchDepthInput()).toBeDisabled();
  });

  it('disables Keep Hierarchy and Match Depth when the filter is invalid', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery({ filterQuery: '{ a } && { b }' })} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(getKeepHierarchyButton('On')).toBeDisabled();
    expect(getMatchDepthInput()).toBeDisabled();
  });

  it('enables Keep Hierarchy and Match Depth once a valid filter is set', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery({ filterQuery: '{ status = error }' })} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(getKeepHierarchyButton('On')).not.toBeDisabled();
    expect(getMatchDepthInput()).not.toBeDisabled();
  });

  it('disables the Ancestor Depth input when keepHierarchy is falsy', async () => {
    const onChange = jest.fn();
    render(
      <TraceIdOptions query={baseQuery({ filterQuery: '{ status = error }', keepHierarchy: false })} onChange={onChange} />
    );

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(getAncestorDepthInput()).toBeDisabled();
  });

  it('keeps the Ancestor Depth value bound when keepHierarchy toggles from true to false', async () => {
    const onChange = jest.fn();
    const { rerender } = render(
      <TraceIdOptions
        query={baseQuery({ filterQuery: '{ status = error }', keepHierarchy: true, ancestorDepth: 5 })}
        onChange={onChange}
      />
    );

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(getAncestorDepthInput()).toHaveValue(5);
    expect(getAncestorDepthInput()).not.toBeDisabled();

    rerender(
      <TraceIdOptions
        query={baseQuery({ filterQuery: '{ status = error }', keepHierarchy: false, ancestorDepth: 5 })}
        onChange={onChange}
      />
    );

    expect(getAncestorDepthInput()).toHaveValue(5);
    expect(getAncestorDepthInput()).toBeDisabled();
  });

  it('clears only the filter fields when Clear Filter is clicked', async () => {
    const onChange = jest.fn();
    render(
      <TraceIdOptions
        query={baseQuery({
          filterQuery: '{ status = error }',
          keepHierarchy: true,
          matchDepth: 2,
          ancestorDepth: 3,
          spanPruning: false,
          spanPruningGroupBy: 'db.sql.table',
        })}
        onChange={onChange}
      />
    );

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));
    await user.click(screen.getByRole('button', { name: 'Clear Filter' }));

    expect(onChange).toHaveBeenCalledWith({
      refId: 'A',
      queryType: 'traceId',
      filterQuery: undefined,
      keepHierarchy: undefined,
      matchDepth: undefined,
      ancestorDepth: undefined,
      spanPruning: false,
      spanPruningGroupBy: 'db.sql.table',
    });
  });

  it('propagates Filter changes as the user types, without requiring blur or Enter', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery()} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    const filterInput = screen.getByPlaceholderText('{ resource.service.name = "checkout" }');
    await user.type(filterInput, 'a');

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ filterQuery: 'a' }));
  });

  it('auto-closes brackets typed in the Filter field, wired end-to-end', async () => {
    function StatefulTraceIdOptions() {
      const [query, setQuery] = React.useState(baseQuery());
      return <TraceIdOptions query={query} onChange={setQuery} />;
    }

    render(<StatefulTraceIdOptions />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    const filterInput = screen.getByPlaceholderText('{ resource.service.name = "checkout" }') as HTMLInputElement;
    fireEvent.focus(filterInput);
    // fireEvent, not userEvent.type: userEvent keeps its own internal cursor model for simulated
    // typing and resets the real DOM selection to match it once our keydown handler returns, which
    // clobbers the setSelectionRange call this feature makes. That's an artifact of userEvent's
    // bookkeeping, not something a real browser does, so it's not appropriate for asserting on
    // caret position specifically -- confirmed by logging the DOM selection immediately after our
    // handler runs (correctly 1) versus after userEvent's own post-processing (reset to 2).
    fireEvent.keyDown(filterInput, { key: '{' });

    expect(filterInput).toHaveValue('{}');
    expect(filterInput.selectionStart).toBe(1);
  });

  it('shows an invalid state for the Filter field when filterQuery combines multiple spansets', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery({ filterQuery: '{ a } && { b }' })} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(screen.getByRole('alert')).toBeInTheDocument();
  });

  it('suppresses the invalid banner for the Filter field while it has focus, and shows it again on blur', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery({ filterQuery: '{ a } && { b }' })} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(screen.getByRole('alert')).toBeInTheDocument();

    const filterInput = screen.getByPlaceholderText('{ resource.service.name = "checkout" }');
    await user.click(filterInput);

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    await user.tab();

    expect(screen.getByRole('alert')).toBeInTheDocument();
  });

  it('shows no invalid state for the Filter field when filterQuery is a valid single spanset filter', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery({ filterQuery: '{ status = error }' })} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows no invalid state for the Filter field when filterQuery is empty', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery()} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText('Filter Options'));

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('marks Keep Hierarchy, Match Depth, and Ancestor Depth as ignored in the collapsed summary when there is no valid filter', () => {
    const onChange = jest.fn();
    render(
      <TraceIdOptions query={baseQuery({ keepHierarchy: true, matchDepth: 2, ancestorDepth: 3 })} onChange={onChange} />
    );

    expect(screen.getByText('Keep Hierarchy: Yes (ignored)')).toBeInTheDocument();
    expect(screen.getByText('Match Depth: 2 (ignored)')).toBeInTheDocument();
    expect(screen.getByText('Ancestor Depth: 3 (ignored)')).toBeInTheDocument();
  });

  it('does not mark Keep Hierarchy, Match Depth, or Ancestor Depth as ignored once a valid filter is set', () => {
    const onChange = jest.fn();
    render(
      <TraceIdOptions
        query={baseQuery({
          filterQuery: '{ status = error }',
          keepHierarchy: true,
          matchDepth: 2,
          ancestorDepth: 3,
        })}
        onChange={onChange}
      />
    );

    expect(screen.getByText('Keep Hierarchy: Yes')).toBeInTheDocument();
    expect(screen.getByText('Match Depth: 2')).toBeInTheDocument();
    expect(screen.getByText('Ancestor Depth: 3')).toBeInTheDocument();
  });

  it('reveals fields from both the Span Pruning Options and Filter Options groups once expanded', async () => {
    const onChange = jest.fn();
    render(<TraceIdOptions query={baseQuery()} onChange={onChange} />);

    expect(screen.queryByText('Group By')).not.toBeInTheDocument();
    expect(screen.queryByText('Ancestor Depth')).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByText('Span Pruning Options'));

    expect(screen.getByText('Group By')).toBeInTheDocument();
    expect(screen.getByText('Ancestor Depth')).toBeInTheDocument();
  });
});
