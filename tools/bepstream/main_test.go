package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	bespb "github.com/example/bep-demo/tools/bepstream/proto"
)

func TestProtodelimUnmarshal(t *testing.T) {
	t.Run("valid message", func(t *testing.T) {
		event := &bespb.BuildEvent{
			LastMessage: true,
			Payload: &bespb.BuildEvent_Progress{
				Progress: &bespb.Progress{},
			},
		}
		data := encodeDelimitedMessage(t, event)

		got := &bespb.BuildEvent{}
		err := protodelim.UnmarshalFrom(bytes.NewReader(data), got)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.LastMessage {
			t.Error("expected LastMessage to be true")
		}
	})

	t.Run("empty reader returns EOF", func(t *testing.T) {
		got := &bespb.BuildEvent{}
		err := protodelim.UnmarshalFrom(bytes.NewReader(nil), got)
		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
	})

	t.Run("truncated message body", func(t *testing.T) {
		event := &bespb.BuildEvent{LastMessage: true}
		data := encodeDelimitedMessage(t, event)
		truncated := data[:len(data)-2]

		got := &bespb.BuildEvent{}
		err := protodelim.UnmarshalFrom(bytes.NewReader(truncated), got)
		if err == nil {
			t.Error("expected error for truncated message")
		}
	})

	t.Run("multiple messages in sequence", func(t *testing.T) {
		events := []*bespb.BuildEvent{
			{Payload: &bespb.BuildEvent_Progress{Progress: &bespb.Progress{}}},
			{Payload: &bespb.BuildEvent_Progress{Progress: &bespb.Progress{}}},
			{LastMessage: true},
		}
		var buf bytes.Buffer
		for _, e := range events {
			buf.Write(encodeDelimitedMessage(t, e))
		}
		reader := bytes.NewReader(buf.Bytes())

		for i := range events {
			got := &bespb.BuildEvent{}
			err := protodelim.UnmarshalFrom(reader, got)
			if err != nil {
				t.Fatalf("message %d: unexpected error: %v", i, err)
			}
			if got.LastMessage != events[i].LastMessage {
				t.Errorf("message %d: LastMessage mismatch", i)
			}
		}
	})
}

func TestGetTargetLabel(t *testing.T) {
	tests := []struct {
		name string
		id   *bespb.BuildEventId
		want string
	}{
		{
			name: "nil id",
			id:   nil,
			want: "<unknown>",
		},
		{
			name: "TargetConfigured",
			id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_TargetConfigured{
					TargetConfigured: &bespb.BuildEventId_TargetConfiguredId{
						Label: "//pkg:target",
					},
				},
			},
			want: "//pkg:target",
		},
		{
			name: "TargetCompleted",
			id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_TargetCompleted{
					TargetCompleted: &bespb.BuildEventId_TargetCompletedId{
						Label: "//other:lib",
					},
				},
			},
			want: "//other:lib",
		},
		{
			name: "TestResult",
			id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_TestResult{
					TestResult: &bespb.BuildEventId_TestResultId{
						Label: "//test:unit_test",
					},
				},
			},
			want: "//test:unit_test",
		},
		{
			name: "TestSummary",
			id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_TestSummary{
					TestSummary: &bespb.BuildEventId_TestSummaryId{
						Label: "//test:integration_test",
					},
				},
			},
			want: "//test:integration_test",
		},
		{
			name: "unknown type",
			id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_Progress{
					Progress: &bespb.BuildEventId_ProgressId{},
				},
			},
			want: "<unknown>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTargetLabel(tt.id)
			if got != tt.want {
				t.Errorf("getTargetLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessEvent(t *testing.T) {
	t.Run("Started event", func(t *testing.T) {
		startTime := time.Now()
		event := &bespb.BuildEvent{
			Payload: &bespb.BuildEvent_Started{
				Started: &bespb.BuildStarted{
					Uuid:      "test-uuid-123",
					Command:   "build",
					StartTime: timestamppb.New(startTime),
				},
			},
		}
		stats := &buildStats{}

		processEvent(event, stats)

		if !stats.buildStarted {
			t.Error("expected buildStarted to be true")
		}
		if stats.uuid != "test-uuid-123" {
			t.Errorf("uuid = %q, want %q", stats.uuid, "test-uuid-123")
		}
		if stats.command != "build" {
			t.Errorf("command = %q, want %q", stats.command, "build")
		}
		if stats.startTime.Unix() != startTime.Unix() {
			t.Errorf("startTime mismatch")
		}
	})

	t.Run("Finished event", func(t *testing.T) {
		finishTime := time.Now()
		event := &bespb.BuildEvent{
			Payload: &bespb.BuildEvent_Finished{
				Finished: &bespb.BuildFinished{
					ExitCode:   &bespb.BuildFinished_ExitCode{Code: 0},
					FinishTime: timestamppb.New(finishTime),
				},
			},
		}
		stats := &buildStats{}

		processEvent(event, stats)

		if !stats.buildFinished {
			t.Error("expected buildFinished to be true")
		}
		if stats.exitCode != 0 {
			t.Errorf("exitCode = %d, want 0", stats.exitCode)
		}
	})

	t.Run("Progress event increments counter", func(t *testing.T) {
		event := &bespb.BuildEvent{
			Payload: &bespb.BuildEvent_Progress{
				Progress: &bespb.Progress{},
			},
		}
		stats := &buildStats{}

		processEvent(event, stats)
		processEvent(event, stats)
		processEvent(event, stats)

		if stats.progressEvents != 3 {
			t.Errorf("progressEvents = %d, want 3", stats.progressEvents)
		}
	})

	t.Run("Completed event success", func(t *testing.T) {
		event := &bespb.BuildEvent{
			Id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_TargetCompleted{
					TargetCompleted: &bespb.BuildEventId_TargetCompletedId{
						Label: "//pkg:lib",
					},
				},
			},
			Payload: &bespb.BuildEvent_Completed{
				Completed: &bespb.TargetComplete{Success: true},
			},
		}
		stats := &buildStats{}

		processEvent(event, stats)

		if stats.targetsBuilt != 1 {
			t.Errorf("targetsBuilt = %d, want 1", stats.targetsBuilt)
		}
		if stats.targetsFailed != 0 {
			t.Errorf("targetsFailed = %d, want 0", stats.targetsFailed)
		}
	})

	t.Run("Completed event failure", func(t *testing.T) {
		event := &bespb.BuildEvent{
			Id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_TargetCompleted{
					TargetCompleted: &bespb.BuildEventId_TargetCompletedId{
						Label: "//pkg:broken",
					},
				},
			},
			Payload: &bespb.BuildEvent_Completed{
				Completed: &bespb.TargetComplete{Success: false},
			},
		}
		stats := &buildStats{}

		processEvent(event, stats)

		if stats.targetsBuilt != 1 {
			t.Errorf("targetsBuilt = %d, want 1", stats.targetsBuilt)
		}
		if stats.targetsFailed != 1 {
			t.Errorf("targetsFailed = %d, want 1", stats.targetsFailed)
		}
	})

	t.Run("Action event increments counter", func(t *testing.T) {
		event := &bespb.BuildEvent{
			Payload: &bespb.BuildEvent_Action{
				Action: &bespb.ActionExecuted{
					Success: true,
					Label:   "//pkg:lib",
					Type:    "Javac",
				},
			},
		}
		stats := &buildStats{}

		processEvent(event, stats)

		if stats.actionsExecuted != 1 {
			t.Errorf("actionsExecuted = %d, want 1", stats.actionsExecuted)
		}
	})

	t.Run("TestSummary passed", func(t *testing.T) {
		event := &bespb.BuildEvent{
			Id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_TestSummary{
					TestSummary: &bespb.BuildEventId_TestSummaryId{
						Label: "//test:my_test",
					},
				},
			},
			Payload: &bespb.BuildEvent_TestSummary{
				TestSummary: &bespb.TestSummary{
					OverallStatus: bespb.TestStatus_PASSED,
				},
			},
		}
		stats := &buildStats{}

		processEvent(event, stats)

		if stats.testsRun != 1 {
			t.Errorf("testsRun = %d, want 1", stats.testsRun)
		}
		if stats.testsPassed != 1 {
			t.Errorf("testsPassed = %d, want 1", stats.testsPassed)
		}
		if stats.testsFailed != 0 {
			t.Errorf("testsFailed = %d, want 0", stats.testsFailed)
		}
	})

	t.Run("TestSummary failed", func(t *testing.T) {
		event := &bespb.BuildEvent{
			Id: &bespb.BuildEventId{
				Id: &bespb.BuildEventId_TestSummary{
					TestSummary: &bespb.BuildEventId_TestSummaryId{
						Label: "//test:failing_test",
					},
				},
			},
			Payload: &bespb.BuildEvent_TestSummary{
				TestSummary: &bespb.TestSummary{
					OverallStatus: bespb.TestStatus_FAILED,
				},
			},
		}
		stats := &buildStats{}

		processEvent(event, stats)

		if stats.testsRun != 1 {
			t.Errorf("testsRun = %d, want 1", stats.testsRun)
		}
		if stats.testsPassed != 0 {
			t.Errorf("testsPassed = %d, want 0", stats.testsPassed)
		}
		if stats.testsFailed != 1 {
			t.Errorf("testsFailed = %d, want 1", stats.testsFailed)
		}
	})
}

func TestBuildStats(t *testing.T) {
	t.Run("printSummary does not panic with all fields", func(t *testing.T) {
		startTime := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
		endTime := time.Date(2024, 1, 15, 10, 1, 30, 0, time.UTC)

		stats := &buildStats{
			buildStarted:    true,
			buildFinished:   true,
			startTime:       startTime,
			endTime:         endTime,
			uuid:            "abc-123",
			command:         "test",
			exitCode:        0,
			targetsBuilt:    5,
			targetsFailed:   1,
			testsRun:        10,
			testsPassed:     8,
			testsFailed:     2,
			actionsExecuted: 42,
			progressEvents:  100,
		}

		stats.printSummary()
	})

	t.Run("printSummary does not panic with empty stats", func(t *testing.T) {
		stats := &buildStats{}
		stats.printSummary()
	})

	t.Run("printSummary handles partial timestamps", func(t *testing.T) {
		stats := &buildStats{
			buildStarted:  true,
			buildFinished: false,
		}
		stats.printSummary()
	})
}

func TestStreamReader(t *testing.T) {
	t.Run("reads complete message", func(t *testing.T) {
		event := &bespb.BuildEvent{
			LastMessage: true,
			Payload: &bespb.BuildEvent_Progress{
				Progress: &bespb.Progress{},
			},
		}
		data := encodeDelimitedMessage(t, event)

		tmpFile, err := os.CreateTemp("", "bep-test-*.bin")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write(data); err != nil {
			t.Fatal(err)
		}
		if _, err := tmpFile.Seek(0, 0); err != nil {
			t.Fatal(err)
		}

		reader := newStreamReader(tmpFile, streamOptions{
			follow:       false,
			pollInterval: 10 * time.Millisecond,
			timeout:      100 * time.Millisecond,
		})

		got, err := reader.readDelimitedMessage()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.LastMessage {
			t.Error("expected LastMessage to be true")
		}
	})

	t.Run("follow mode waits for data", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "bep-follow-*.bin")
		if err != nil {
			t.Fatal(err)
		}
		tmpName := tmpFile.Name()
		defer os.Remove(tmpName)
		tmpFile.Close()

		writeFile, err := os.OpenFile(tmpName, os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			t.Fatal(err)
		}

		readFile, err := os.Open(tmpName)
		if err != nil {
			t.Fatal(err)
		}
		defer readFile.Close()

		reader := newStreamReader(readFile, streamOptions{
			follow:       true,
			pollInterval: 10 * time.Millisecond,
			timeout:      1 * time.Second,
		})

		event := &bespb.BuildEvent{
			LastMessage: true,
			Payload: &bespb.BuildEvent_Progress{
				Progress: &bespb.Progress{},
			},
		}
		data := encodeDelimitedMessage(t, event)

		done := make(chan *bespb.BuildEvent, 1)
		go func() {
			got, err := reader.readDelimitedMessage()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			done <- got
		}()

		time.Sleep(50 * time.Millisecond)
		if _, err := writeFile.Write(data); err != nil {
			t.Fatal(err)
		}
		writeFile.Close()

		select {
		case got := <-done:
			if !got.LastMessage {
				t.Error("expected LastMessage to be true")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for message")
		}
	})
}

func encodeDelimitedMessage(t *testing.T, event *bespb.BuildEvent) []byte {
	t.Helper()
	data, err := proto.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	var buf bytes.Buffer
	sizeBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(sizeBuf, uint64(len(data)))
	buf.Write(sizeBuf[:n])
	buf.Write(data)
	return buf.Bytes()
}
