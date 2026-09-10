package contract_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const (
	commandOutputLimit  = 1 << 20
	commandChunk        = 4 << 10
	commandFloodBytes   = 16 << 20
	commandExitFailure  = 7
	commandReadyTimeout = 3 * time.Second
	commandTimeout      = time.Second
	commandPoll         = 5 * time.Millisecond
)

func payloadArgs(mode string, n int, root string) []string {
	return []string{"-test.run=^TestHardeningHelper$", "--", hardeningMarker, "payload", mode, strconv.Itoa(n), root}
}

func runCommandPayload(t *testing.T, args []string) {
	if len(args) != 3 {
		t.Fatal("payload arguments")
	}
	stop := scenarioWatchdog("command payload")
	mode, root := args[0], args[2]
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 0 || n > commandFloodBytes {
		t.Fatal("payload size")
	}
	if mode == "exit" {
		stop()
		syscall.Exit(commandExitFailure)
	}
	if mode == "stdout" || mode == "both" {
		writePayload(t, os.Stdout, n)
	}
	if mode == "stderr" || mode == "both" {
		writePayload(t, os.Stderr, n)
	}
	if mode == "flood" || mode == "slow" {
		writePayload(t, os.Stdout, commandChunk)
		writePayload(t, os.Stderr, commandChunk)
		if err := os.WriteFile(filepath.Join(root, "ready"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		for sent := commandChunk; sent < commandFloodBytes; sent += commandChunk {
			writePayload(t, os.Stdout, commandChunk)
			writePayload(t, os.Stderr, commandChunk)
			if mode == "slow" {
				time.Sleep(commandPoll)
			}
		}
		time.Sleep(helperScenarioTimeout)
	}
	stop()
	// This producer only emits synthetic bytes. Bypass test/coverage exit
	// trailers so the measured streams remain exact, including under -cover.
	// The parent and the command runner still use normal race/coverage reporting.
	syscall.Exit(0)
}

func writePayload(t *testing.T, file *os.File, n int) {
	t.Helper()
	chunk := bytes.Repeat([]byte("x"), min(commandChunk, n))
	for n > 0 {
		size := min(n, len(chunk))
		if _, err := file.Write(chunk[:size]); err != nil {
			t.Fatal(err)
		}
		n -= size
	}
}

func runResourceCommands(t *testing.T) {
	stop := scenarioWatchdog("commands")
	defer stop()
	policy, err := platform.NewEnvPolicy(nil, map[string]string{"GORACE": "atexit_sleep_ms=0"})
	if err != nil {
		t.Fatal(err)
	}
	runner := platform.NewOSCommandRunner(commandReadyTimeout)
	for _, mode := range []string{"stdout", "stderr", "both"} {
		for _, size := range []int{commandOutputLimit - 1, commandOutputLimit, commandOutputLimit + 1} {
			spec := platform.CommandSpec{Path: os.Args[0], Args: payloadArgs(mode, size, ""), Env: policy}
			got, err := runner.Run(t.Context(), spec)
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr := 0, 0
			if mode != "stderr" {
				stdout = size
			}
			if mode != "stdout" {
				stderr = size
			}
			if len(got.Stdout) != min(stdout, commandOutputLimit) || len(got.Stderr) != min(stderr, commandOutputLimit) ||
				got.StdoutTruncated != (stdout > commandOutputLimit) || got.StderrTruncated != (stderr > commandOutputLimit) {
				t.Fatalf("%s/%d boundary", mode, size)
			}
			if !bytes.Equal(got.Stdout, bytes.Repeat([]byte("x"), len(got.Stdout))) ||
				!bytes.Equal(got.Stderr, bytes.Repeat([]byte("x"), len(got.Stderr))) {
				t.Fatal("retained prefix changed")
			}
		}
	}
	for _, mode := range []string{missingFixtureName, "exit"} {
		spec := platform.CommandSpec{Path: os.Args[0], Args: payloadArgs(mode, 0, ""), Env: policy}
		if mode == missingFixtureName {
			spec.Path = filepath.Join(t.TempDir(), missingFixtureName)
		}
		got, err := runner.Run(t.Context(), spec)
		if err == nil || got.TimedOut || got.StdoutTruncated || got.StderrTruncated {
			t.Fatalf("%s: %+v %v", mode, got, err)
		}
		if mode == "exit" && got.ExitCode != commandExitFailure {
			t.Fatal("exit code lost")
		}
	}
	for _, mode := range []string{"flood", "slow"} {
		for _, cancelCaller := range []bool{true, false} {
			root := t.TempDir()
			spec := platform.CommandSpec{Path: os.Args[0], Args: payloadArgs(mode, 0, root), Env: policy, Timeout: commandTimeout}
			ctx, cancel := context.WithCancel(t.Context())
			type outcome struct {
				result platform.ExecResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() { r, e := runner.Run(ctx, spec); done <- outcome{r, e} }()
			var got outcome
			func() {
				joined := false
				defer func() {
					cancel()
					if !joined {
						<-done
					}
				}()
				pid := waitCommandReady(t, root)
				if cancelCaller {
					cancel()
				}
				got = <-done
				joined = true
				var status syscall.WaitStatus
				if _, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
					t.Fatalf("direct child not reaped: %v", err)
				}
			}()
			want := context.DeadlineExceeded
			if cancelCaller {
				want = context.Canceled
			}
			if !errors.Is(got.err, want) || got.result.TimedOut == cancelCaller {
				t.Fatalf("%s cancellation classification: %v", mode, got.err)
			}
			if len(got.result.Stdout) < commandChunk || len(got.result.Stderr) < commandChunk ||
				len(got.result.Stdout) > commandOutputLimit || len(got.result.Stderr) > commandOutputLimit {
				t.Fatal("flood/partial output bounds")
			}
		}
	}
	hardeningEvent(t, map[string]any{eventKind: "resource_case", "scenario": "commands", "stream_limit": commandOutputLimit,
		"source_limit": commandFloodBytes, "owned_children_remaining": 0})
}

func waitCommandReady(t *testing.T, root string) int {
	t.Helper()
	deadline := time.Now().Add(commandReadyTimeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(root, "ready"))
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(commandPoll)
	}
	t.Fatal("command readiness deadline")
	return 0
}

func checkRepeatedDescriptors(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	before := count()
	for range resourceRepeats {
		r, err := platform.NewScopedOSReader(root)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := r.ReadFile(t.Context(), "file")
		_, missingErr := r.ReadFile(t.Context(), missingFixtureName)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, cancelErr := r.ReadFile(ctx, "file")
		closeErr := r.Close()
		if readErr != nil || !errors.Is(missingErr, os.ErrNotExist) || !errors.Is(cancelErr, context.Canceled) || closeErr != nil {
			t.Fatal("reader lifecycle")
		}
	}
	if after := count(); after != before {
		t.Fatalf("descriptor leak before=%d after=%d", before, after)
	}
	hardeningEvent(t, map[string]any{eventKind: "resource_case", "scenario": "descriptors", "before": before, "after": count()})
}

func checkRepeatedCommands(t *testing.T) {
	t.Helper()
	policy, err := platform.NewEnvPolicy(nil, map[string]string{"GORACE": "atexit_sleep_ms=0"})
	if err != nil {
		t.Fatal(err)
	}
	runner := platform.NewOSCommandRunner(commandReadyTimeout)
	const modes = 3
	for i := 0; i < resourceRepeats; i++ {
		mode := "stdout"
		if i%modes == 1 {
			mode = "exit"
		}
		ctx, cancel := context.WithCancel(t.Context())
		if i%modes == modes-1 {
			cancel()
		}
		got, err := runner.Run(ctx, platform.CommandSpec{Path: os.Args[0], Args: payloadArgs(mode, 0, ""), Env: policy})
		cancel()
		switch i % modes {
		case 0:
			if err != nil || got.ExitCode != 0 {
				t.Fatal("runner reuse success")
			}
		case 1:
			if err == nil || got.ExitCode != commandExitFailure {
				t.Fatal("runner reuse failure")
			}
		default:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("runner reuse cancellation")
			}
		}
		assertNoProbeWorkers(t)
	}
}
