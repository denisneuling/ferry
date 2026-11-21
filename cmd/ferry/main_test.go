package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		code       int
		stdout     string
		stderrPart string
	}{
		{name: "no command", args: nil, code: 2, stderrPart: "Usage:"},
		{name: "version", args: []string{"version"}, code: 0, stdout: "ferry dev (commit unknown, built unknown)\n"},
		{name: "help", args: []string{"help"}, code: 0, stdout: usage},
		{name: "unknown", args: []string{"sail"}, code: 2, stderrPart: `unknown command "sail"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != tc.code {
				t.Fatalf("exit code %d, want %d", code, tc.code)
			}
			if tc.stdout != "" && stdout.String() != tc.stdout {
				t.Errorf("stdout %q, want %q", stdout.String(), tc.stdout)
			}
			if !strings.Contains(stderr.String(), tc.stderrPart) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), tc.stderrPart)
			}
		})
	}
}
