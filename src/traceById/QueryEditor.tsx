import { css } from '@emotion/css';
import { defaults } from 'lodash';

import { type GrafanaTheme2, type QueryEditorProps } from '@grafana/data';
import { Icon, useStyles2 } from '@grafana/ui';

import { type TempoDatasource } from '../datasource';
import { defaultQuery, type MyDataSourceOptions, type TempoQuery } from '../types';

import { TraceIdInput } from './TraceIdInput';
import { TraceIdOptions } from './TraceIdOptions';

type Props = QueryEditorProps<TempoDatasource, TempoQuery, MyDataSourceOptions>;

export function QueryEditor(props: Props) {
  const styles = useStyles2(getStyles);
  const query = defaults(props.query, defaultQuery);
  const message = getTraceMessage(props.data, query.refId);

  return (
    <>
      <TraceIdInput query={query} onChange={props.onChange} onRunQuery={props.onRunQuery} />
      <div className={styles.optionsContainer}>
        <TraceIdOptions query={query} onChange={props.onChange} app={props.app} />
      </div>
      {message && (
        <div className={styles.message} role="status">
          <Icon name="exclamation-triangle" size="sm" /> {message}
        </div>
      )}
    </>
  );
}

function getTraceMessage(data: Props['data'], refId: string): string | undefined {
  const message = data?.series.find((frame) => frame.refId === refId)?.meta?.custom?.message;
  return typeof message === 'string' && message ? message : undefined;
}

const getStyles = (theme: GrafanaTheme2) => ({
  message: css({
    marginTop: theme.spacing(1),
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.warning.text,
  }),
  optionsContainer: css({
    marginTop: '10px',
  }),
});
