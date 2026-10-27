package main

import (
	"bytes"
	"context"
	"testing"
)

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
