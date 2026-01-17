// Package main provides a BEP (Build Event Protocol) stream reader.
// It reads Bazel's build event binary file and outputs a summary of events.
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/protobuf/proto"

	bespb "github.com/example/bep-demo/tools/bepstream/proto"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <bep-binary-file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nRun bazel with: bazel build --build_event_binary_file=/tmp/bep.bin //...\n")
		os.Exit(1)
	}

	filename := os.Args[1]
	if err := streamBEP(filename); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func streamBEP(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	eventCount := 0
	stats := &buildStats{}

	fmt.Println("=== BEP Stream Summary ===")
	fmt.Println()

	for {
		event, err := readDelimitedMessage(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read message: %w", err)
		}

		eventCount++
		processEvent(event, stats)

		if event.LastMessage {
			fmt.Println("[Last message received]")
			break
		}
	}

	fmt.Println()
	fmt.Println("=== Build Statistics ===")
	fmt.Printf("Total events: %d\n", eventCount)
	stats.printSummary()

	return nil
}

func readDelimitedMessage(reader *bufio.Reader) (*bespb.BuildEvent, error) {
	size, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, size)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return nil, fmt.Errorf("failed to read message body: %w", err)
	}

	event := &bespb.BuildEvent{}
	opts := proto.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := opts.Unmarshal(buf, event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	return event, nil
}

type buildStats struct {
	buildStarted    bool
	buildFinished   bool
	startTime       time.Time
	endTime         time.Time
	uuid            string
	command         string
	exitCode        int32
	targetsBuilt    int
	targetsFailed   int
	testsRun        int
	testsPassed     int
	testsFailed     int
	actionsExecuted int
	progressEvents  int
}

func (s *buildStats) printSummary() {
	if s.uuid != "" {
		fmt.Printf("Build UUID: %s\n", s.uuid)
	}
	if s.command != "" {
		fmt.Printf("Command: %s\n", s.command)
	}
	if s.buildStarted && s.buildFinished {
		duration := s.endTime.Sub(s.startTime)
		fmt.Printf("Duration: %v\n", duration.Round(time.Millisecond))
	}
	fmt.Printf("Exit code: %d\n", s.exitCode)
	fmt.Printf("Actions executed: %d\n", s.actionsExecuted)
	fmt.Printf("Targets built: %d (failed: %d)\n", s.targetsBuilt, s.targetsFailed)
	if s.testsRun > 0 {
		fmt.Printf("Tests: %d passed, %d failed (total: %d)\n", s.testsPassed, s.testsFailed, s.testsRun)
	}
	fmt.Printf("Progress events: %d\n", s.progressEvents)
}

func processEvent(event *bespb.BuildEvent, stats *buildStats) {
	switch p := event.Payload.(type) {
	case *bespb.BuildEvent_Started:
		stats.buildStarted = true
		stats.uuid = p.Started.Uuid
		stats.command = p.Started.Command
		if p.Started.StartTime != nil {
			stats.startTime = p.Started.StartTime.AsTime()
		}
		fmt.Printf("▶ Build started: %s (UUID: %s)\n", p.Started.Command, p.Started.Uuid)

	case *bespb.BuildEvent_Finished:
		stats.buildFinished = true
		stats.exitCode = p.Finished.ExitCode.Code
		if p.Finished.FinishTime != nil {
			stats.endTime = p.Finished.FinishTime.AsTime()
		}
		fmt.Printf("■ Build finished: exit code %d\n", p.Finished.ExitCode.Code)

	case *bespb.BuildEvent_Progress:
		stats.progressEvents++

	case *bespb.BuildEvent_Configured:
		fmt.Printf("  ◇ Target configured: %s\n", getTargetLabel(event.Id))

	case *bespb.BuildEvent_Completed:
		stats.targetsBuilt++
		success := p.Completed.Success
		label := getTargetLabel(event.Id)
		if success {
			fmt.Printf("  ✓ Target completed: %s\n", label)
		} else {
			stats.targetsFailed++
			fmt.Printf("  ✗ Target failed: %s\n", label)
		}

	case *bespb.BuildEvent_Action:
		stats.actionsExecuted++
		if !p.Action.Success {
			fmt.Printf("  ✗ Action failed: %s (%s)\n", p.Action.Label, p.Action.Type)
		}

	case *bespb.BuildEvent_TestResult:
		testLabel := getTargetLabel(event.Id)
		status := p.TestResult.Status
		fmt.Printf("  ⚡ Test result: %s - %s\n", testLabel, status.String())

	case *bespb.BuildEvent_TestSummary:
		stats.testsRun++
		testLabel := getTargetLabel(event.Id)
		status := p.TestSummary.OverallStatus
		if status == bespb.TestStatus_PASSED {
			stats.testsPassed++
			fmt.Printf("  ✓ Test passed: %s\n", testLabel)
		} else {
			stats.testsFailed++
			fmt.Printf("  ✗ Test failed: %s (%s)\n", testLabel, status.String())
		}

	case *bespb.BuildEvent_Aborted:
		fmt.Printf("  ⚠ Aborted: %s - %s\n", p.Aborted.Reason.String(), p.Aborted.Description)

	case *bespb.BuildEvent_Configuration:
		fmt.Printf("  ⚙ Configuration: %s (cpu: %s)\n", p.Configuration.Mnemonic, p.Configuration.Cpu)

	case *bespb.BuildEvent_BuildToolLogs:
		fmt.Println("  📋 Build tool logs available")

	case *bespb.BuildEvent_BuildMetrics:
		if p.BuildMetrics.ActionSummary != nil {
			fmt.Printf("  📊 Metrics: %d actions\n", p.BuildMetrics.ActionSummary.ActionsExecuted)
		}
	}
}

func getTargetLabel(id *bespb.BuildEventId) string {
	if id == nil {
		return "<unknown>"
	}
	switch i := id.Id.(type) {
	case *bespb.BuildEventId_TargetConfigured:
		return i.TargetConfigured.Label
	case *bespb.BuildEventId_TargetCompleted:
		return i.TargetCompleted.Label
	case *bespb.BuildEventId_TestResult:
		return i.TestResult.Label
	case *bespb.BuildEventId_TestSummary:
		return i.TestSummary.Label
	default:
		return "<unknown>"
	}
}
