package qtcore_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qutetest"
)

// Qt's reentrant classes (QRegularExpression and its match types) work on
// any goroutine while the application runs: each goroutine with its own
// objects, which GC still frees through the GUI thread. A class that is not
// reentrant still panics off the GUI thread.
func TestReentrantOffGUIThread(t *testing.T) {
	const workers, rounds = 8, 200
	before := qutetest.Freed()
	var wg sync.WaitGroup
	errs := make(chan string, workers)
	for w := range workers {
		wg.Go(func() {
			re := qtcore.NewQRegularExpressionWithPattern(`(?<word>\w+)-(\d+)`)
			if !re.IsValid() {
				errs <- "invalid pattern: " + re.ErrorString()
				return
			}
			for i := range rounds {
				subject := fmt.Sprintf("w%d-%d x-1 y-2", w, i)
				m := re.Match(subject)
				if !m.HasMatch() || m.CapturedWithName("word") != fmt.Sprintf("w%d", w) || m.CapturedFull(2) != fmt.Sprint(i) {
					errs <- fmt.Sprintf("worker %d round %d: match %q", w, i, m.Captured())
					return
				}
				n := 0
				for it := re.GlobalMatch(subject); it.HasNext(); n++ {
					it.Next()
				}
				if n != 3 {
					errs <- fmt.Sprintf("worker %d: %d global matches, want 3", w, n)
					return
				}
			}
			if s := qtcore.QRegularExpressionEscape("a.b"); s != `a\.b` {
				errs <- "Escape: " + s
			}
			re.Delete()
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	qutetest.CollectGarbage()
	if freed := qutetest.Freed() - before; freed < workers*rounds {
		t.Errorf("GC freed %d of the matches made off the GUI thread, want at least %d", freed, workers*rounds)
	}

	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "off the GUI thread") {
			t.Errorf("a QTimer made off the GUI thread: panic %q, want an off-thread message", msg)
		}
	}()
	qtcore.NewQTimer(nil) // the test goroutine is not the GUI thread
}
