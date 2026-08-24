import { css } from '@emotion/css';
import * as React from 'react';
import { useToggle } from 'react-use';

import { type GrafanaTheme2 } from '@grafana/data';
import { EditorField, EditorRow } from '@grafana/plugin-ui';
import { AutoSizeInput, RadioButtonGroup, useStyles2 } from '@grafana/ui';

import { QueryOptionGroup } from '../_importedDependencies/datasources/prometheus/QueryOptionGroup';
import { type TempoQuery } from '../types';

interface Props {
  onChange: (value: TempoQuery) => void;
  query: Partial<TempoQuery> & TempoQuery;
}

/**
 * Parse a string value to an integer, returning undefined when the conversion fails, for example for an empty field.
 * Undefined is deliberate rather than a client-side fallback: an unset field means the query param is omitted, which
 * is what lets Tempo own the defaults (5 for min spans, 1 for max parent depth).
 *
 * Values outside the safe integer range are also treated as unset. They serialize in exponential notation (1e+21),
 * which fails to unmarshal into the int64 these params are decoded into, failing the whole query with an error that
 * points nowhere near the field the user typed in.
 */
const parseOptionalInt = (val: string): number | undefined => {
  const parsed = parseInt(val, 10);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
};

export const TraceIdQueryOptions = React.memo<Props>(({ onChange, query }) => {
  const styles = useStyles2(getStyles);
  const [isOpen, toggleOpen] = useToggle(false);

  // Keep this default paired with the Go-side default in pkg/tempo/trace.go (appendSpanPruningParams);
  // there is no shared source of truth between them.
  const spanPruningEnabled = query.spanPruning ?? true;

  const onSpanPruningChange = (val: boolean) => {
    onChange({ ...query, spanPruning: val });
  };
  const onGroupByChange = (e: React.FormEvent<HTMLInputElement>) => {
    const groupBy = e.currentTarget.value.trim();
    onChange({ ...query, spanPruningGroupBy: groupBy === '' ? undefined : groupBy });
  };
  const onMinSpansChange = (e: React.FormEvent<HTMLInputElement>) => {
    onChange({ ...query, spanPruningMinSpans: parseOptionalInt(e.currentTarget.value) });
  };
  const onMaxParentDepthChange = (e: React.FormEvent<HTMLInputElement>) => {
    onChange({ ...query, spanPruningMaxParentDepth: parseOptionalInt(e.currentTarget.value) });
  };

  // The sub-parameters are omitted while pruning is off because the backend drops them, so showing them would
  // misreport what the query actually sends.
  const collapsedInfo = spanPruningEnabled
    ? [
        'Span Pruning: On',
        `Group By: ${query.spanPruningGroupBy || 'auto'}`,
        `Min Spans: ${query.spanPruningMinSpans ?? 'auto'}`,
        `Max Parent Depth: ${query.spanPruningMaxParentDepth ?? 'auto'}`,
      ]
    : ['Span Pruning: Off'];

  return (
    <EditorRow>
      <div className={styles.options}>
        <QueryOptionGroup
          title="TraceID Query Options"
          collapsedInfo={collapsedInfo}
          isOpen={isOpen}
          onToggle={toggleOpen}
        >
          <EditorField
            label="Span Pruning"
            tooltip="Collapses groups of similar leaf spans into a single summary span when Tempo returns the trace. On by default."
          >
            <RadioButtonGroup<boolean>
              options={[
                { label: 'On', value: true },
                { label: 'Off', value: false },
              ]}
              value={spanPruningEnabled}
              onChange={onSpanPruningChange}
            />
          </EditorField>
          <EditorField
            label="Group By Attributes"
            tooltip="Comma-separated attribute glob patterns deciding which leaf spans belong in the same group, for example db.*,http.method. Leave blank to group similar spans by name only."
          >
            <AutoSizeInput
              className="width-16"
              placeholder="db.*,http.method"
              type="string"
              disabled={!spanPruningEnabled}
              onCommitChange={onGroupByChange}
              value={query.spanPruningGroupBy ?? ''}
            />
          </EditorField>
          <EditorField
            label="Min Spans"
            tooltip="Minimum number of similar spans required in a group before they are collapsed. Must be at least 2. Leave blank to let Tempo apply its default of 5."
          >
            <AutoSizeInput
              className="width-4"
              placeholder="5"
              type="number"
              min={2}
              disabled={!spanPruningEnabled}
              onCommitChange={onMinSpansChange}
              value={query.spanPruningMinSpans ?? ''}
            />
          </EditorField>
          <EditorField
            label="Max Parent Depth"
            tooltip="How many ancestor levels above the aggregated leaf spans can also be aggregated. 0 aggregates leaves only and -1 means unlimited depth. Leave blank to let Tempo apply its default of 1."
          >
            <AutoSizeInput
              className="width-4"
              placeholder="1"
              type="number"
              min={-1}
              disabled={!spanPruningEnabled}
              onCommitChange={onMaxParentDepthChange}
              value={query.spanPruningMaxParentDepth ?? ''}
            />
          </EditorField>
        </QueryOptionGroup>
      </div>
    </EditorRow>
  );
});

TraceIdQueryOptions.displayName = 'TraceIdQueryOptions';

const getStyles = (theme: GrafanaTheme2) => {
  return {
    options: css({
      display: 'flex',
      width: '-webkit-fill-available',
      gap: theme.spacing(1),

      '> div': {
        width: 'auto',
      },
    }),
  };
};
