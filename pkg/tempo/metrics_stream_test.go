package tempo

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-tempo-datasource/pkg/tempo/kinds/dataquery"
	"github.com/grafana/tempo/pkg/tempopb"
	v1 "github.com/grafana/tempo/pkg/tempopb/common/v1"
	"google.golang.org/grpc/metadata"
)

func TestProcessInstantMetricsStream_DoneIncludesLastResult(t *testing.T) {
	logger := backend.NewLoggerWith("logger", "tsdb.tempo.test")
	ds := &DataSource{logger: logger}

	metrics := &tempopb.SearchMetrics{CompletedJobs: 1, TotalJobs: 1}
	stream := &mockInstantMetricsStreamer{
		responses: []*tempopb.QueryInstantResponse{
			{
				Series: []*tempopb.InstantSeries{
					{
						Value: 1.23,
						Labels: []v1.KeyValue{
							{Key: "service", Value: &v1.AnyValue{Value: &v1.AnyValue_StringValue{StringValue: "frontend"}}},
						},
					},
				},
				Metrics: metrics,
			},
			{
				Series: []*tempopb.InstantSeries{
					{
						Value: 4.56,
						Labels: []v1.KeyValue{
							{Key: "service", Value: &v1.AnyValue{Value: &v1.AnyValue_StringValue{StringValue: "frontend"}}},
						},
					},
				},
				Metrics: metrics,
			},
		},
	}
	sender := &mockSender{}

	err := ds.processInstantMetricsStream(context.Background(), stream, sender)
	if err != nil {
		t.Fatalf("Expected no error, got %s", err)
	}

	assertDoneKeepsLastStreamingResult(t, sender.responses)
}

func TestProcessMetricsStream_DoneIncludesLastResult(t *testing.T) {
	logger := backend.NewLoggerWith("logger", "tsdb.tempo.test")
	ds := &DataSource{logger: logger}

	metrics := &tempopb.SearchMetrics{CompletedJobs: 1, TotalJobs: 1}
	stream := &mockRangeMetricsStreamer{
		responses: []*tempopb.QueryRangeResponse{
			{
				Series: []*tempopb.TimeSeries{
					{
						Labels: []v1.KeyValue{
							{Key: "service", Value: &v1.AnyValue{Value: &v1.AnyValue_StringValue{StringValue: "frontend"}}},
						},
						Samples: []tempopb.Sample{
							{TimestampMs: 1638316800000, Value: 1.23},
						},
					},
				},
				Metrics: metrics,
			},
			{
				Series: []*tempopb.TimeSeries{
					{
						Labels: []v1.KeyValue{
							{Key: "service", Value: &v1.AnyValue{Value: &v1.AnyValue_StringValue{StringValue: "frontend"}}},
						},
						Samples: []tempopb.Sample{
							{TimestampMs: 1638316800000, Value: 4.56},
						},
					},
				},
				Metrics: metrics,
			},
		},
	}
	sender := &mockSender{}

	err := ds.processMetricsStream(context.Background(), "{} | rate()", stream, sender)
	if err != nil {
		t.Fatalf("Expected no error, got %s", err)
	}

	assertDoneKeepsLastStreamingResult(t, sender.responses)
}

func assertDoneKeepsLastStreamingResult(t *testing.T, responses []*data.Frame) {
	t.Helper()

	if len(responses) < 2 {
		t.Fatalf("Expected at least one streaming response and one Done, got %d", len(responses))
	}

	lastStreaming := responses[len(responses)-2]
	done := responses[len(responses)-1]

	streamingState := lastStreaming.Fields[2].At(0).(string)
	if streamingState != string(dataquery.SearchStreamingStateStreaming) {
		t.Fatalf("Expected penultimate state streaming, got %q", streamingState)
	}

	doneState := done.Fields[2].At(0).(string)
	if doneState != string(dataquery.SearchStreamingStateDone) {
		t.Fatalf("Expected final state done, got %q", doneState)
	}

	streamingResult := lastStreaming.Fields[0].At(0).(json.RawMessage)
	doneResult := done.Fields[0].At(0).(json.RawMessage)

	if string(doneResult) == "null" || len(doneResult) == 0 {
		t.Fatalf("Expected Done to include last result payload, got %s", string(doneResult))
	}

	if string(streamingResult) != string(doneResult) {
		t.Fatalf("Expected Done result to match last streaming result.\nstreaming: %s\ndone: %s", streamingResult, doneResult)
	}

	streamingMetrics := lastStreaming.Fields[1].At(0).(json.RawMessage)
	doneMetrics := done.Fields[1].At(0).(json.RawMessage)
	if string(streamingMetrics) != string(doneMetrics) {
		t.Fatalf("Expected Done metrics to match last streaming metrics.\nstreaming: %s\ndone: %s", streamingMetrics, doneMetrics)
	}
}

type mockInstantMetricsStreamer struct {
	responses []*tempopb.QueryInstantResponse
	index     int
}

func (m *mockInstantMetricsStreamer) Recv() (*tempopb.QueryInstantResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockInstantMetricsStreamer) Header() (metadata.MD, error) { panic("implement me") }
func (m *mockInstantMetricsStreamer) Trailer() metadata.MD         { panic("implement me") }
func (m *mockInstantMetricsStreamer) CloseSend() error             { panic("implement me") }
func (m *mockInstantMetricsStreamer) Context() context.Context     { panic("implement me") }
func (m *mockInstantMetricsStreamer) SendMsg(any) error            { panic("implement me") }
func (m *mockInstantMetricsStreamer) RecvMsg(any) error            { panic("implement me") }

type mockRangeMetricsStreamer struct {
	responses []*tempopb.QueryRangeResponse
	index     int
}

func (m *mockRangeMetricsStreamer) Recv() (*tempopb.QueryRangeResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockRangeMetricsStreamer) Header() (metadata.MD, error) { panic("implement me") }
func (m *mockRangeMetricsStreamer) Trailer() metadata.MD         { panic("implement me") }
func (m *mockRangeMetricsStreamer) CloseSend() error             { panic("implement me") }
func (m *mockRangeMetricsStreamer) Context() context.Context     { panic("implement me") }
func (m *mockRangeMetricsStreamer) SendMsg(any) error            { panic("implement me") }
func (m *mockRangeMetricsStreamer) RecvMsg(any) error            { panic("implement me") }
