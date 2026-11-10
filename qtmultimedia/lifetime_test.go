package qtmultimedia_test

import (
	"testing"

	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/internal/rt"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtmultimedia"
)

// A player does not own its audio output or video sink; with no Go variable
// left, only the player's kept references hold them through a collection.
func TestPlayerKeepsOutputsThroughGC(t *testing.T) {
	var player *qtmultimedia.QMediaPlayer
	qute.Run(t, func() {
		player = qtmultimedia.NewQMediaPlayer(nil)
		player.SetAudioOutput(qtmultimedia.NewQAudioOutput(nil))
		player.SetVideoSink(qtmultimedia.NewQVideoSink(nil))
	})
	qute.CollectGarbage()
	qute.Run(t, func() {
		if out := player.AudioOutput(); out == nil || !out.Alive() {
			t.Error("audio output freed while the player uses it")
		}
		if sink := player.VideoSink(); sink == nil || !sink.Alive() {
			t.Error("video sink freed while the player uses it")
		}
		player.Delete()
	})
}

// ~QVideoSink drops its own connections before ~QObject emits destroyed; the
// runtime's death notice must survive that, or the handle would dangle.
func TestVideoSinkDeathSeen(t *testing.T) {
	qute.Run(t, func() {
		root := qtcore.NewQObject(nil)
		sink := qtmultimedia.NewQVideoSink(root)
		root.Delete()
		if sink.Alive() {
			t.Error("video sink handle alive after its parent deleted it")
		}
	})
}

// Video frames hold pixel memory outside the Go heap; each frame counts it.
func TestVideoFrameMemoryCounted(t *testing.T) {
	qute.Run(t, func() {
		img := qtgui.NewQImageWithSize(qtcore.QSize{Width: 100, Height: 100}, qtgui.QImageFormat_ARGB32)
		before := rt.MemoryLive()
		f := qtmultimedia.NewQVideoFrameWithImage(img)
		if got := rt.MemoryLive() - before; got != 100*100*4 {
			t.Errorf("a 100x100 frame counts %d bytes, want %d", got, 100*100*4)
		}
		f.Delete()
		if got := rt.MemoryLive() - before; got != 0 {
			t.Errorf("%d bytes still counted after Delete", got)
		}
	})
}
