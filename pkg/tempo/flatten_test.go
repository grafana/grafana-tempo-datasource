package tempo

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/stretchr/testify/require"
)

func TestFlattenTimeSeriesToTabular_TwoSeries(t *testing.T) {
	t1 := time.Unix(1700000000, 0)
	t2 := time.Unix(1700000060, 0)

	frameA := data.NewFrame("series-a",
		data.NewField("time", nil, []time.Time{t1}),
		data.NewField("{service=\"a\"}", data.Labels{"service": "a"}, []float64{1.5}),
	)
	frameB := data.NewFrame("series-b",
		data.NewField("time", nil, []time.Time{t2}),
		data.NewField("{service=\"b\"}", data.Labels{"service": "b"}, []float64{2.5}),
	)

	out := flattenTimeSeriesToTabular(data.Frames{frameA, frameB})
	require.Len(t, out, 1)

	f := out[0]
	require.Equal(t, "timestamp", f.Fields[0].Name)
	require.Equal(t, "value", f.Fields[1].Name)
	require.Equal(t, "service", f.Fields[2].Name)
	require.Equal(t, 2, f.Rows())

	ts, _ := f.Fields[0].ConcreteAt(0)
	require.Equal(t, t1, ts.(time.Time))
	val, _ := f.Fields[1].ConcreteAt(0)
	require.Equal(t, 1.5, val.(float64))
	svc, _ := f.Fields[2].ConcreteAt(0)
	require.Equal(t, "a", svc.(string))

	ts2, _ := f.Fields[0].ConcreteAt(1)
	require.Equal(t, t2, ts2.(time.Time))
	val2, _ := f.Fields[1].ConcreteAt(1)
	require.Equal(t, 2.5, val2.(float64))
	svc2, _ := f.Fields[2].ConcreteAt(1)
	require.Equal(t, "b", svc2.(string))
}

func TestFlattenTimeSeriesToTabular_MultipleLabelsPerRow(t *testing.T) {
	t1 := time.Unix(1700000000, 0)
	labels := data.Labels{"service": "frontend", "env": "prod"}

	frame := data.NewFrame("series",
		data.NewField("time", nil, []time.Time{t1}),
		data.NewField("rate", labels, []float64{1.0}),
	)

	out := flattenTimeSeriesToTabular(data.Frames{frame})
	require.Len(t, out, 1)
	f := out[0]
	require.Equal(t, 1, f.Rows())

	// Without distinct string pointers, both columns would read "prod" (last v in the inner loop).
	svcField, _ := f.FieldByName("service")
	svc, _ := svcField.ConcreteAt(0)
	require.Equal(t, "frontend", svc.(string))
	envField, _ := f.FieldByName("env")
	env, _ := envField.ConcreteAt(0)
	require.Equal(t, "prod", env.(string))
}

func TestFlattenTimeSeriesToTabular_MultipleSamplesPerSeries(t *testing.T) {
	t1 := time.Unix(1700000000, 0)
	t2 := time.Unix(1700000060, 0)

	frame := data.NewFrame("series",
		data.NewField("time", nil, []time.Time{t1, t2}),
		data.NewField("rate", data.Labels{"service": "api"}, []float64{1.0, 3.5}),
	)

	out := flattenTimeSeriesToTabular(data.Frames{frame})
	require.Len(t, out, 1)
	f := out[0]
	require.Equal(t, 2, f.Rows())

	valueField, _ := f.FieldByName("value")
	v0, _ := valueField.ConcreteAt(0)
	require.Equal(t, 1.0, v0.(float64))
	v1, _ := valueField.ConcreteAt(1)
	require.Equal(t, 3.5, v1.(float64))
}

func TestFlattenTimeSeriesToTabular_SkipsExemplarFrames(t *testing.T) {
	t1 := time.Unix(1700000000, 0)

	series := data.NewFrame("series",
		data.NewField("time", nil, []time.Time{t1}),
		data.NewField("value", data.Labels{"service": "a"}, []float64{1.0}),
	)
	exemplar := data.NewFrame("exemplar",
		data.NewField("Time", nil, []time.Time{t1}),
		data.NewField("Value", nil, []float64{1.0}),
		data.NewField("traceId", nil, []string{"abc"}),
	)
	exemplar.Meta = &data.FrameMeta{DataTopic: data.DataTopicAnnotations}

	out := flattenTimeSeriesToTabular(data.Frames{series, exemplar})
	require.Len(t, out, 1)
	require.Equal(t, 1, out[0].Rows())
}
