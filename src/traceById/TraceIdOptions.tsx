import { css } from '@emotion/css';
import * as React from 'react';
import { useToggle } from 'react-use';

import { type CoreApp, type GrafanaTheme2 } from '@grafana/data';
import { EditorField } from '@grafana/plugin-ui';
import { AutoSizeInput, Box, Button, RadioButtonGroup, useStyles2 } from '@grafana/ui';

import { QueryOptionGroup } from '../_importedDependencies/datasources/prometheus/QueryOptionGroup';
import { type TempoQuery } from '../types';

import { computeBracketAutoClose } from './bracketAutoClose';
import { validateFilterQuery } from './validateFilterQuery';

interface Props {
  onChange: (value: TempoQuery) => void;
  query: TempoQuery;
  app?: CoreApp;
}

const parseOptionalInt = (val: string): number | undefined => {
  if (val.trim() === '') {
    return undefined;
  }
  const parsed = parseInt(val, 10);
  return isNaN(parsed) ? undefined : parsed;
};

export const TraceIdOptions = React.memo<Props>(({ onChange, query }) => {
  const styles = useStyles2(getStyles);
  const [isOpen, toggleOpen] = useToggle(false);
  const filterError = validateFilterQuery(query.filterQuery ?? '');
  const hasValidFilter = !!query.filterQuery?.trim() && !filterError;
  const [isFilterFocused, setIsFilterFocused] = React.useState(false);
  const filterInputRef = React.useRef<HTMLInputElement>(null);
  const pendingCaretRef = React.useRef<number | null>(null);

  React.useLayoutEffect(() => {
    if (pendingCaretRef.current !== null) {
      filterInputRef.current?.setSelectionRange(pendingCaretRef.current, pendingCaretRef.current);
      pendingCaretRef.current = null;
    }
  });

  const onSpanPruningChange = (value: boolean) => {
    onChange({ ...query, spanPruning: value });
  };
  const onSpanPruningGroupByChange = (event: React.FormEvent<HTMLInputElement>) => {
    onChange({ ...query, spanPruningGroupBy: event.currentTarget.value });
  };
  const onSpanPruningMinSpansChange = (event: React.FormEvent<HTMLInputElement>) => {
    onChange({ ...query, spanPruningMinSpans: parseOptionalInt(event.currentTarget.value) });
  };
  const onSpanPruningMaxParentDepthChange = (event: React.FormEvent<HTMLInputElement>) => {
    onChange({ ...query, spanPruningMaxParentDepth: parseOptionalInt(event.currentTarget.value) });
  };

  const onFilterQueryChange = (event: React.FormEvent<HTMLInputElement>) => {
    onChange({ ...query, filterQuery: event.currentTarget.value });
  };
  const onFilterQueryKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    const { selectionStart, selectionEnd, value } = event.currentTarget;
    if (selectionStart === null || selectionEnd === null || selectionStart !== selectionEnd) {
      return;
    }

    const edit = computeBracketAutoClose(value, selectionStart, event.key);
    if (!edit) {
      return;
    }

    event.preventDefault();
    pendingCaretRef.current = edit.caret;
    if (edit.value !== value) {
      onChange({ ...query, filterQuery: edit.value });
    } else {
      filterInputRef.current?.setSelectionRange(edit.caret, edit.caret);
      pendingCaretRef.current = null;
    }
  };
  const onKeepHierarchyChange = (value: boolean) => {
    onChange({ ...query, keepHierarchy: value });
  };
  const onMatchDepthChange = (event: React.FormEvent<HTMLInputElement>) => {
    onChange({ ...query, matchDepth: parseOptionalInt(event.currentTarget.value) });
  };
  const onAncestorDepthChange = (event: React.FormEvent<HTMLInputElement>) => {
    onChange({ ...query, ancestorDepth: parseOptionalInt(event.currentTarget.value) });
  };
  const onClearFilter = () => {
    onChange({
      ...query,
      filterQuery: undefined,
      keepHierarchy: undefined,
      matchDepth: undefined,
      ancestorDepth: undefined,
    });
  };

  const collapsedSpanPruningOptions = [
    `On/Off: ${(query.spanPruning ?? true) ? 'On' : 'Off'}`,
    `Group By: ${query.spanPruningGroupBy || 'none'}`,
    `Min Spans: ${query.spanPruningMinSpans ?? 5}`,
    `Max Parent Depth: ${query.spanPruningMaxParentDepth ?? 1}`,
  ];

  const collapsedFilterOptions = [
    `Filter: ${query.filterQuery || 'none'}`,
    `Keep Hierarchy: ${query.keepHierarchy ? 'Yes' : 'No'}${hasValidFilter ? '' : ' (ignored)'}`,
    `Match Depth: ${query.matchDepth ?? 0}${hasValidFilter ? '' : ' (ignored)'}`,
    `Ancestor Depth: ${query.ancestorDepth ?? -1}${hasValidFilter && query.keepHierarchy ? '' : ' (ignored)'}`,
  ];

  return (
    <Box backgroundColor="secondary" borderRadius="default">
      <div className={styles.options}>
        <QueryOptionGroup
          title="Span Pruning Options"
          collapsedInfo={collapsedSpanPruningOptions}
          isOpen={isOpen}
          onToggle={toggleOpen}
        >
          <EditorField label="On/Off" tooltip="Turn span pruning on or off for this query.">
            <RadioButtonGroup
              options={[
                { label: 'On', value: true },
                { label: 'Off', value: false },
              ]}
              value={query.spanPruning ?? true}
              onChange={onSpanPruningChange}
            />
          </EditorField>
          <EditorField
            label="Group By"
            tooltip="Comma-separated attribute names used to group spans for pruning, e.g. db.sql.table or http.method. Defaults to none — groups are formed by span name, kind, and status alone, with no attribute constraint."
          >
            <AutoSizeInput
              minWidth={40}
              placeholder="db.sql.table, http.method"
              type="string"
              spellCheck={false}
              defaultValue={query.spanPruningGroupBy}
              onCommitChange={onSpanPruningGroupByChange}
              value={query.spanPruningGroupBy}
            />
          </EditorField>
          <EditorField
            label="Min Spans"
            tooltip="Minimum number of similar spans required in a group before pruning is applied. Groups smaller than this are preserved as-is. Default: 5."
          >
            <AutoSizeInput
              className="width-4"
              placeholder="5"
              type="number"
              min={0}
              defaultValue={query.spanPruningMinSpans}
              onCommitChange={onSpanPruningMinSpansChange}
              value={query.spanPruningMinSpans}
            />
          </EditorField>
          <EditorField
            label="Max Parent Depth"
            tooltip="How many ancestor levels above the aggregated leaf spans can also be aggregated. Use 0 to aggregate only leaves, -1 for unlimited depth, or n for a bounded depth. Default: 1."
          >
            <AutoSizeInput
              className="width-4"
              placeholder="1"
              type="number"
              min={-1}
              defaultValue={query.spanPruningMaxParentDepth}
              onCommitChange={onSpanPruningMaxParentDepthChange}
              value={query.spanPruningMaxParentDepth}
            />
          </EditorField>
        </QueryOptionGroup>

        <QueryOptionGroup
          title="Filter Options"
          collapsedInfo={collapsedFilterOptions}
          isOpen={isOpen}
          onToggle={toggleOpen}
        >
          <EditorField
            label="Filter"
            tooltip='A TraceQL spanset filter over spans in the trace, e.g. { status = error && name = "foo" }. Only one { ... } block is supported — no chained filters or pipelines. Leave empty to skip filtering.'
            invalid={!isFilterFocused && !!filterError}
            error={!isFilterFocused ? filterError : undefined}
          >
            <AutoSizeInput
              ref={filterInputRef}
              minWidth={60}
              placeholder='{ resource.service.name = "checkout" }'
              type="string"
              spellCheck={false}
              invalid={!!filterError}
              onFocus={() => setIsFilterFocused(true)}
              onBlur={() => setIsFilterFocused(false)}
              onChange={onFilterQueryChange}
              onKeyDown={onFilterQueryKeyDown}
              value={query.filterQuery ?? ''}
            />
          </EditorField>
          <EditorField
            label="Keep Hierarchy"
            tooltip="Include each matched span's ancestor path back to the root, so the result renders as a complete waterfall instead of just the isolated matching spans. Ignored unless Filter is set to a valid filter."
          >
            <RadioButtonGroup
              options={[
                { label: 'On', value: true },
                { label: 'Off', value: false },
              ]}
              value={query.keepHierarchy ?? false}
              onChange={onKeepHierarchyChange}
              disabled={!hasValidFilter}
            />
          </EditorField>
          <EditorField
            label="Match Depth"
            tooltip="How many levels of descendants to keep below each span matched by Filter, independent of Keep Hierarchy. Use -1 for the full subtree, 0 for the matched spans alone, or n for a bounded depth. Ignored unless Filter is set to a valid filter. Default: 0."
          >
            <AutoSizeInput
              className="width-4"
              placeholder="0"
              type="number"
              min={-1}
              defaultValue={query.matchDepth}
              onCommitChange={onMatchDepthChange}
              value={query.matchDepth}
              disabled={!hasValidFilter}
            />
          </EditorField>
          <EditorField
            label="Ancestor Depth"
            tooltip="How many levels of ancestors to keep above each matched span. Use -1 for the whole path to the root, 0 for none, or n for a bounded depth. Only read when Keep Hierarchy is enabled and Filter is set to a valid filter. Default: -1."
          >
            <AutoSizeInput
              className="width-4"
              placeholder="-1"
              type="number"
              min={-1}
              defaultValue={query.ancestorDepth}
              onCommitChange={onAncestorDepthChange}
              value={query.ancestorDepth}
              disabled={!hasValidFilter || !query.keepHierarchy}
            />
          </EditorField>
        </QueryOptionGroup>

        <Button
          className={styles.clearFilterButton}
          variant="secondary"
          size="sm"
          icon="times"
          onClick={onClearFilter}
        >
          Clear Filter
        </Button>
      </div>
    </Box>
  );
});

TraceIdOptions.displayName = 'TraceIdOptions';

const getStyles = (theme: GrafanaTheme2) => {
  return {
    options: css({
      display: 'flex',
      flexDirection: 'column',
      width: '-webkit-fill-available',
      gap: theme.spacing(1),
    }),
    clearFilterButton: css({
      alignSelf: 'flex-end',
    }),
  };
};
