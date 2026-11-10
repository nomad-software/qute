package qute_test

import (
	"testing"

	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qutetest"
)

func TestProcessEventsRunsPostedWork(t *testing.T) {
	qutetest.Run(t, func() {
		ran := false
		qute.Post(func() { ran = true })
		qute.ProcessEvents()
		if !ran {
			t.Error("work posted to the GUI thread did not run in ProcessEvents")
		}
		if !qute.IsGUIThread() {
			t.Error("IsGUIThread() = false inside qutetest.Run")
		}
	})
}
