package tempo

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/tempo/pkg/tempopb"
	v1 "github.com/grafana/tempo/pkg/tempopb/common/v1"
	"github.com/stretchr/testify/require"
)

// Metrics fixtures shared by the differential tests: the query, the series Tempo returns per policy, and helpers to
// compare the HTTP and the streamed frames.
const metricsDiffQuery = `{} | rate() by (resource.service.name)`

var metricsDiffServices = map[string]float64{"frontend": 1.5, "backend": 2.5}

func filterServices(policy string) []string {
	want := strings.TrimPrefix(policy, "service=")
	var out []string
	for svc := range metricsDiffServices {
		if policy == "" || svc == want {
			out = append(out, svc)
		}
	}
	sort.Strings(out)
	return out
}

func serviceLabels(svc string) []v1.KeyValue {
	return []v1.KeyValue{{Key: "resource.service.name", Value: &v1.AnyValue{Value: &v1.AnyValue_StringValue{StringValue: svc}}}}
}

func rangeResponse(policy string) *tempopb.QueryRangeResponse {
	resp := &tempopb.QueryRangeResponse{Metrics: &tempopb.SearchMetrics{}}
	for _, svc := range filterServices(policy) {
		resp.Series = append(resp.Series, &tempopb.TimeSeries{
			Labels: serviceLabels(svc),
			Samples: []tempopb.Sample{
				{TimestampMs: 1_000, Value: metricsDiffServices[svc]},
				{TimestampMs: 2_000, Value: metricsDiffServices[svc] + 1},
			},
		})
	}
	return resp
}

func instantResponse(policy string) *tempopb.QueryInstantResponse {
	resp := &tempopb.QueryInstantResponse{Metrics: &tempopb.SearchMetrics{}}
	for _, svc := range filterServices(policy) {
		resp.Series = append(resp.Series, &tempopb.InstantSeries{Labels: serviceLabels(svc), Value: metricsDiffServices[svc]})
	}
	return resp
}

// summarize reduces frames to a comparable form. The instant frame's time column is time.Now(), so it is left out.
func summarize(t *testing.T, frames []*data.Frame) []string {
	t.Helper()
	out := make([]string, 0, len(frames))
	for _, fr := range frames {
		value := fr.Fields[len(fr.Fields)-1]
		vals := make([]float64, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			vals = append(vals, value.At(i).(float64))
		}
		var times []int64
		if value.Len() > 1 {
			for i := 0; i < fr.Fields[0].Len(); i++ {
				times = append(times, fr.Fields[0].At(i).(time.Time).UnixMilli())
			}
		}
		out = append(out, fmt.Sprintf("%s|%s|%v|%v", fr.Name, value.Labels, vals, times))
	}
	sort.Strings(out)
	return out
}

// streamedFrames reads the last streamed packet (state done), which carries the final frames.
func streamedFrames(t *testing.T, packets []json.RawMessage) []*data.Frame {
	t.Helper()
	require.NotEmpty(t, packets)
	var packet data.Frame
	require.NoError(t, json.Unmarshal(packets[len(packets)-1], &packet))

	state, _ := packet.FieldByName("state")
	require.Equal(t, "done", state.At(0))
	errField, _ := packet.FieldByName("error")
	require.Empty(t, errField.At(0))

	result, _ := packet.FieldByName("result")
	raw := result.At(0).(json.RawMessage)
	if len(raw) == 0 {
		return nil
	}
	var frames []*data.Frame
	require.NoError(t, json.Unmarshal(raw, &frames))
	return frames
}
