package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"gogap/internal/domain"
)

func TestRunVersionPrintsProjectVersion(t *testing.T) {
	var logs bytes.Buffer
	oldLogOutput := log.Writer()
	oldLogFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldLogOutput)
		log.SetFlags(oldLogFlags)
	})

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe returned error: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = oldStdout })

	if err := run([]string{"--version"}); err != nil {
		t.Fatalf("run(--version) returned error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}

	if got, want := string(output), "GoGap 1.0.0\n"; got != want {
		t.Fatalf("run(--version) output = %q, want %q", got, want)
	}
	if logs.Len() != 0 {
		t.Fatalf("run(--version) should not start normal logging, got:\n%s", logs.String())
	}
}

func TestStartupReadyLoggerLogsOnce(t *testing.T) {
	var logs bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	})

	logger := newStartupReadyLogger("127.0.0.1:8080", 2*time.Minute)
	logger(domain.SnapshotResponse{Progress: &domain.ProgressState{Label: "拉取场内行情", Percent: 20}})
	logger(domain.SnapshotResponse{Progress: &domain.ProgressState{Label: "拉取完成", Percent: 100}, Items: []domain.FundSnapshot{{Code: "501019"}}})
	logger(domain.SnapshotResponse{Progress: &domain.ProgressState{Label: "拉取完成", Percent: 100}, Items: []domain.FundSnapshot{{Code: "501019"}, {Code: "164824"}}})

	output := logs.String()
	if strings.Contains(output, "拉取场内行情") {
		t.Fatalf("startup completion logged before ready snapshot:\n%s", output)
	}
	want := "GoGap ready: url=http://127.0.0.1:8080 rows=1 poll=2m0s"
	if strings.Count(output, want) != 1 {
		t.Fatalf("expected exactly one startup completion log %q, got:\n%s", want, output)
	}
}
