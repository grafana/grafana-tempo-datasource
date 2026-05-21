package tempo

import (
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// flattenTimeSeriesToTabular converts TraceQL metrics multi-frame time series responses
// (one frame per label combination) into a single flat tabular frame with timestamp, value,
// and label columns for Grafana SQL / schemads compatibility.
func flattenTimeSeriesToTabular(frames data.Frames) data.Frames {
	if len(frames) == 0 {
		return frames
	}

	type row struct {
		t      time.Time
		value  *float64
		labels data.Labels
	}

	var rows []row
	labelKeysSet := map[string]struct{}{}

	for _, frame := range frames {
		if shouldSkipMetricsFlattenFrame(frame) {
			continue
		}

		timeField := metricsTimeField(frame)
		if timeField == nil {
			continue
		}

		for _, f := range frame.Fields {
			if !f.Type().Numeric() {
				continue
			}
			for k := range f.Labels {
				labelKeysSet[k] = struct{}{}
			}
			for i := 0; i < f.Len(); i++ {
				tv := timeField.At(i)
				t, ok := tv.(time.Time)
				if !ok {
					continue
				}
				v, err := f.FloatAt(i)
				if err != nil {
					continue
				}
				rows = append(rows, row{
					t:      t,
					value:  copyFloat64Ptr(v),
					labels: f.Labels,
				})
			}
		}
	}

	if len(rows) == 0 {
		return frames
	}

	labelKeys := make([]string, 0, len(labelKeysSet))
	for k := range labelKeysSet {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].t.Before(rows[j].t)
	})

	timestamps := make([]time.Time, len(rows))
	values := make([]*float64, len(rows))
	labelCols := make(map[string][]*string, len(labelKeys))
	for _, k := range labelKeys {
		labelCols[k] = make([]*string, len(rows))
	}

	for i, r := range rows {
		timestamps[i] = r.t
		values[i] = r.value
		for _, k := range labelKeys {
			if v, ok := r.labels[k]; ok {
				labelCols[k][i] = copyStringPtr(v)
			}
		}
	}

	fields := make([]*data.Field, 0, 2+len(labelKeys))
	fields = append(fields,
		data.NewField("timestamp", nil, timestamps),
		data.NewField("value", nil, values),
	)
	for _, k := range labelKeys {
		fields = append(fields, data.NewField(k, nil, labelCols[k]))
	}

	out := data.NewFrame("", fields...)

	for _, frame := range frames {
		if frame.Meta != nil && frame.Meta.ExecutedQueryString != "" {
			out.Meta = &data.FrameMeta{
				ExecutedQueryString: frame.Meta.ExecutedQueryString,
			}
			break
		}
	}

	return data.Frames{out}
}

// shouldSkipMetricsFlattenFrame returns true for frames that must not be merged into the
// SQL tabular result (timestamp, value, label columns). TraceQL range metrics responses
// include exemplar frames alongside series frames (see traceql.TransformMetricsResponse and
// transformExemplarToFrame): those rows are annotations for graph drill-down (trace IDs),
// not additional metric samples. They have Time/Value fields but a different shape and
// semantics; flattening them would pollute the table and add columns (e.g. traceId) that
// schemads does not advertise. Skipping by Name and DataTopicAnnotations matches how this
// plugin and Grafana mark exemplar data.
func shouldSkipMetricsFlattenFrame(frame *data.Frame) bool {
	if frame == nil {
		return true
	}
	if frame.Name == "exemplar" {
		return true
	}
	if frame.Meta != nil && frame.Meta.DataTopic == data.DataTopicAnnotations {
		return true
	}
	return false
}

func copyStringPtr(s string) *string {
	p := new(string)
	*p = s
	return p
}

func copyFloat64Ptr(v float64) *float64 {
	p := new(float64)
	*p = v
	return p
}

func metricsTimeField(frame *data.Frame) *data.Field {
	for _, f := range frame.Fields {
		if f.Type() == data.FieldTypeTime || f.Type() == data.FieldTypeNullableTime {
			return f
		}
		if f.Name == "time" || f.Name == "Time" {
			return f
		}
	}
	return nil
}
