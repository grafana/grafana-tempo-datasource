import * as React from 'react';

import { EditorField } from '@grafana/plugin-ui';
import { Input } from '@grafana/ui';

import { type TempoQuery } from '../types';

interface Props {
  query: TempoQuery;
  onChange: (value: TempoQuery) => void;
  onRunQuery: () => void;
}

export function TraceIdInput({ query, onChange, onRunQuery }: Props) {
  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      onRunQuery();
    }
  };

  return (
    <EditorField label="Trace ID" tooltip="The hex-encoded trace ID to look up.">
      <Input
        value={query.query || ''}
        onChange={(event) => onChange({ ...query, query: event.currentTarget.value })}
        onKeyDown={onKeyDown}
        placeholder="Enter a trace ID (run with Enter or Shift+Enter)"
      />
    </EditorField>
  );
}
