package main

import (
	"strings"
	"testing"
)

func TestRunJoinAcceptsInvitationBeforeOrAfterFlags(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"tcx1.invalid", "--no-open", "--name", "CLI test"},
		{"--no-open", "--name", "CLI test", "tcx1.invalid"},
	} {
		err := runJoin(args)
		if err == nil {
			t.Fatal("runJoin unexpectedly accepted an invalid invitation")
		}
		if strings.Contains(err.Error(), "requires exactly one") {
			t.Fatalf("runJoin rejected the documented argument order: %v", err)
		}
	}
}

func TestRunJoinRejectsMoreThanOneInvitation(t *testing.T) {
	t.Parallel()

	err := runJoin([]string{"tcx1.first", "tcx1.second"})
	if err == nil || !strings.Contains(err.Error(), "requires exactly one") {
		t.Fatalf("runJoin error = %v, want invitation count error", err)
	}
}
