package main

import (
	"io"
	"os"
	"syscall"
	"testing"

	mihomolog "github.com/metacubex/mihomo/log"
	"github.com/sirupsen/logrus"
)

// TestStdoutNotPollutedByMihomoLogs guards the CI failure mode
// "parse tester output: invalid character 'i' in literal true":
// mihomo's log package points logrus at os.Stdout, so any internal
// warning (e.g. OpenVPN stack errors) would corrupt the JSON output.
// The tester's init must silence mihomo's logger and move logrus to stderr.
func TestStdoutNotPollutedByMihomoLogs(t *testing.T) {
	if got := mihomolog.Level(); got != mihomolog.SILENT {
		t.Fatalf("mihomo log level = %v, want SILENT", got)
	}
	if got := logrus.StandardLogger().Out; got != os.Stderr {
		t.Fatalf("logrus output = %v, want os.Stderr", got)
	}

	// Redirect fd 1 itself so we capture anything written through the
	// original stdout handle that logrus grabbed in mihomo's init.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := syscall.Dup(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Dup2(int(w.Fd()), 1); err != nil {
		t.Fatal(err)
	}

	mihomolog.Infoln("pollution probe")
	mihomolog.Warnln("pollution probe")
	mihomolog.Errorln("pollution probe")
	logrus.Warnln("pollution probe")

	if err := syscall.Dup2(saved, 1); err != nil {
		t.Fatal(err)
	}
	syscall.Close(saved)
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 0 {
		t.Fatalf("stdout polluted with %d bytes: %q", len(out), out)
	}
}
