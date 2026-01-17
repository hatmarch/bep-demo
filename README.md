# Bazel Build Event Protocol (BEP) Demo

This project demonstrates how to consume Bazel's Build Event Protocol (BEP) in real-time by streaming the binary event file.

## Project Structure

- `java/greeter/` - A simple Java binary with unit tests (build target for demonstration)
- `tools/bepstream/` - Go binary that reads and summarizes BEP events
- `tools/bepstream/proto/` - BEP proto definitions from Bazel 8.4.2

## Prerequisites

- Bazel 8.4.2 (see `.bazelversion`)

## Building the Project

```bash
# Build everything
bazel build //...

# Build just the Java binary
bazel build //java/greeter

# Run the Java binary
bazel run //java/greeter -- "Developer"
```

## Running Tests

```bash
bazel test //java/greeter:greeter_test
```

## Using the BEP Stream Reader

### Step 1: Build with BEP output

Run any Bazel command with `--build_event_binary_file` to capture events:

```bash
# Build and capture BEP events
bazel build --build_event_binary_file=/tmp/bep.bin //...

# Or test and capture events (shows test results in BEP)
bazel test --build_event_binary_file=/tmp/bep.bin //java/greeter:greeter_test
```

### Step 2: Analyze the BEP events

```bash
# Build the BEP stream reader
bazel build //tools/bepstream

# Run it against the captured events
bazel-bin/tools/bepstream/bepstream_/bepstream /tmp/bep.bin
```

### Example Output

```
=== BEP Stream Summary ===

▶ Build started: test (UUID: 442fe3be-57be-4c5a-8d9e-257cfc11243a)
  ⚙ Configuration: darwin_arm64-fastbuild (cpu: darwin_arm64)
  ◇ Target configured: //java/greeter:greeter_test
  ✓ Target completed: //java/greeter:greeter_test
  ⚡ Test result: //java/greeter:greeter_test - PASSED
  ✓ Test passed: //java/greeter:greeter_test
■ Build finished: exit code 0
  📋 Build tool logs available
  📊 Metrics: 27 actions
[Last message received]

=== Build Statistics ===
Total events: 33
Build UUID: 442fe3be-57be-4c5a-8d9e-257cfc11243a
Command: test
Duration: 1m26.384s
Exit code: 0
Actions executed: 0
Targets built: 1 (failed: 0)
Tests: 1 passed, 0 failed (total: 1)
Progress events: 9
```

## How It Works

1. Bazel writes build events to a binary file using the `--build_event_binary_file` flag
2. Events are written as length-delimited protocol buffer messages (varint-prefixed)
3. The `bepstream` tool reads these messages using Go's protobuf library
4. Each event type is parsed and summarized to stdout

## BEP Event Types

The Build Event Protocol includes events such as:
- `BuildStarted` - Build invocation begins
- `Progress` - Incremental build progress
- `Configuration` - Build configuration details
- `TargetConfigured` - Target analysis complete
- `ActionExecuted` - Individual action completion
- `TargetComplete` - Target build complete
- `TestResult` - Individual test attempt result
- `TestSummary` - Overall test status
- `BuildMetrics` - Build statistics
- `BuildFinished` - Build invocation complete

## Proto Definitions

The proto files in `tools/bepstream/proto/` are sourced from [Bazel 8.4.2](https://github.com/bazelbuild/bazel/tree/8.4.2):
- `build_event_stream.proto` - Main BEP definitions
- `src/main/protobuf/*.proto` - Supporting protos (command_line, failure_details, etc.)

## References

- [Bazel BEP Documentation](https://bazel.build/remote/bep)
- [BEP Proto Definition](https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/buildeventstream/proto/build_event_stream.proto)
