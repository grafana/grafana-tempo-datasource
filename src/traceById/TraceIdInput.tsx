import * as React from 'react';

import { EditorField } from '@grafana/plugin-ui';
import { Input } from '@grafana/ui';

import { type TempoQuery } from '../types';

import { validateTraceId } from './validateTraceId';

interface Props {
  query: TempoQuery;
  onChange: (value: TempoQuery) => void;
  onRunQuery: () => void;
}

export function TraceIdInput({ query, onChange, onRunQuery }: Props) {
  const [isFocused, setIsFocused] = React.useState(false);
  const traceIdError = validateTraceId(query.query ?? '');

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      onRunQuery();
    }
  };

  return (
    <EditorField
      label="Trace ID"
      tooltip="The trace ID to look up."
      invalid={!isFocused && !!traceIdError}
      error={!isFocused ? traceIdError : undefined}
    >
      <Input
        value={query.query || ''}
        onChange={(event) => onChange({ ...query, query: event.currentTarget.value })}
        onKeyDown={onKeyDown}
        onFocus={() => setIsFocused(true)}
        onBlur={() => setIsFocused(false)}
        invalid={!!traceIdError}
        spellCheck={false}
        placeholder="Enter a trace ID (run with Enter or Shift+Enter)"
      />
    </EditorField>
  );
}
