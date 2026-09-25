import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { type TempoQuery } from '../types';

import { TraceIdInput } from './TraceIdInput';

describe('TraceIdInput', () => {
  function baseQuery(overrides: Partial<TempoQuery> = {}): TempoQuery {
    return { refId: 'A', queryType: 'traceId', ...overrides } as TempoQuery;
  }

  it('shows an invalid state when the trace ID does not start with letters/numbers', async () => {
    const onChange = jest.fn();
    render(<TraceIdInput query={baseQuery({ query: '!not-a-trace-id' })} onChange={onChange} onRunQuery={jest.fn()} />);

    expect(screen.getByRole('alert')).toBeInTheDocument();
  });

  it('shows no invalid state for a plain trace ID', () => {
    const onChange = jest.fn();
    render(<TraceIdInput query={baseQuery({ query: 'abc123' })} onChange={onChange} onRunQuery={jest.fn()} />);

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows no invalid state for an empty query', () => {
    const onChange = jest.fn();
    render(<TraceIdInput query={baseQuery()} onChange={onChange} onRunQuery={jest.fn()} />);

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('allows a trailing space-separated string after a valid trace ID', () => {
    const onChange = jest.fn();
    render(
      <TraceIdInput
        query={baseQuery({ query: 'abc123 anything goes here' })}
        onChange={onChange}
        onRunQuery={jest.fn()}
      />
    );

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('suppresses the invalid banner while focused, and shows it again on blur', async () => {
    const onChange = jest.fn();
    render(<TraceIdInput query={baseQuery({ query: '!bad' })} onChange={onChange} onRunQuery={jest.fn()} />);

    expect(screen.getByRole('alert')).toBeInTheDocument();

    const user = userEvent.setup();
    const input = screen.getByPlaceholderText('Enter a trace ID (run with Enter or Shift+Enter)');
    await user.click(input);

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    await user.tab();

    expect(screen.getByRole('alert')).toBeInTheDocument();
  });
});
