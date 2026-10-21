import { css } from '@emotion/css';
import { defaults } from 'lodash';

import { type QueryEditorProps } from '@grafana/data';
import { useStyles2 } from '@grafana/ui';

import { type TempoDatasource } from '../datasource';
import { defaultQuery, type MyDataSourceOptions, type TempoQuery } from '../types';

import { TraceIdInput } from './TraceIdInput';
import { TraceIdOptions } from './TraceIdOptions';

type Props = QueryEditorProps<TempoDatasource, TempoQuery, MyDataSourceOptions>;

export function QueryEditor(props: Props) {
  const styles = useStyles2(getStyles);
  const query = defaults(props.query, defaultQuery);

  return (
    <>
      <TraceIdInput query={query} onChange={props.onChange} onRunQuery={props.onRunQuery} />
      <div className={styles.optionsContainer}>
        <TraceIdOptions query={query} onChange={props.onChange} app={props.app} />
      </div>
    </>
  );
}

const getStyles = () => ({
  optionsContainer: css({
    marginTop: '10px',
  }),
});
