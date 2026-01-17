// Package main provides a BEP (Build Event Protocol) stream reader.
// It reads Bazel's build event binary file and outputs a summary of events.
// Supports streaming mode (-f) to follow the file as it's being written.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/protobuf/encoding/protodelim"

	bespb "github.com/example/bep-demo/tools/bepstream/proto"
)

var (
	followMode  = flag.Bool("f", false, "Follow mode: wait for new data as the file is being written")
	pollInterval = flag.Duration("poll", 100*time.Millisecond, "Poll interval when following (default 100ms)")
	timeout      = flag.Duration("timeout", 5*time.Minute, "Timeout for follow mode (default 5m)")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [-f] [-poll duration] [-timeout duration] <bep-binary-file>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nRun bazel with: bazel build --build_event_binary_file=/tmp/bep.bin //...\n")
		fmt.Fprintf(os.Stderr, "\nFor streaming mode, start this tool first with -f, then run bazel:\n")
		fmt.Fprintf(os.Stderr, "  Terminal 1: %s -f /tmp/bep.bin\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Terminal 2: bazel build --build_event_binary_file=/tmp/bep.bin //...\n")
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	filename := flag.Arg(0)
	opts := streamOptions{
		follow:       *followMode,
		pollInterval: *pollInterval,
		timeout:      *timeout,
	}
	if err := streamBEP(filename, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type streamOptions struct {
	follow       bool
	pollInterval time.Duration
	timeout      time.Duration
}

func streamBEP(filename string, opts streamOptions) error {
	var file *os.File
	var err error

	if opts.follow {
		file, err = waitForFile(filename, opts.timeout)
	} else {
		file, err = os.Open(filename)
	}
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	reader := newStreamReader(file, opts)
	eventCount := 0
	stats := &buildStats{}

	fmt.Println("=== BEP Stream Summary ===")
	fmt.Println()

	startTime := time.Now()
	for {
		event, err := reader.readDelimitedMessage()
		if err == io.EOF {
			if opts.follow && time.Since(startTime) < opts.timeout {
				time.Sleep(opts.pollInterval)
				continue
			}
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

func waitForFile(filename string, timeout time.Duration) (*os.File, error) {
	deadline := time.Now().Add(timeout)
	pollInterval := 100 * time.Millisecond

	for {
		file, err := os.Open(filename)
		if err == nil {
			return file, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for file %s to be created", filename)
		}
		fmt.Printf("Waiting for %s to be created...\n", filename)
		time.Sleep(pollInterval)
	}
}

type streamReader struct {
	file         *os.File
	reader       *bufio.Reader
	follow       bool
	pollInterval time.Duration
	timeout      time.Duration
}

func newStreamReader(file *os.File, opts streamOptions) *streamReader {
	return &streamReader{
		file:         file,
		reader:       bufio.NewReader(file),
		follow:       opts.follow,
		pollInterval: opts.pollInterval,
		timeout:      opts.timeout,
	}
}

func (r *streamReader) readDelimitedMessage() (*bespb.BuildEvent, error) {
	event := &bespb.BuildEvent{}
	startTime := time.Now()

	for {
		err := protodelim.UnmarshalFrom(r.reader, event)
		if err == nil {
			return event, nil
		}
		if err != io.EOF {
			return nil, fmt.Errorf("failed to read message: %w", err)
		}
		if !r.follow {
			return nil, io.EOF
		}
		if time.Since(startTime) > r.timeout {
			return nil, io.EOF
		}
		time.Sleep(r.pollInterval)
	}
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
