package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestVisiblePlaybackWithoutCECReachesCatalogValidation(t *testing.T) {
	var output bytes.Buffer
	err := run(context.Background(), []string{
		"--state-dir", t.TempDir(), "--temp-dir", t.TempDir(), t.TempDir(),
	}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "no playable media") {
		t.Fatalf("expected catalog validation without requiring CEC, got %v", err)
	}
}

func TestCLIRejectsUnsafeOrIncompleteInvocation(t *testing.T) {
	for _, args := range [][]string{nil, {"/media"}, {"--headless"}, {"--headless", "--state-dir", "state", "--temp-dir", "tmp", "--run-for=-1s", "root"}, {"--unknown"}} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output, &output); err == nil {
			t.Fatalf("accepted args=%v", args)
		}
	}
}
func TestCLIHelpDoesNotStartRuntime(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("--headless")) {
		t.Fatal("missing usage")
	}
}
