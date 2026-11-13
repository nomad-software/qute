package qtwidgets_test

import (
	"testing"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
	"github.com/nomad-software/qute/qutetest"
)

// Qt's static dialog functions run the dialog's own event loop with the
// caller's widget as its parent. A handler that deletes that widget meanwhile
// (a pane whose shell exits) must not crash: Qt's QMessageBox statics built
// the box on the stack, which the parent then destroyed a second time, so
// qute builds it on the heap (shim_manual.h). Each case deletes the parent
// once the dialog is up and checks the function returns.
func TestStaticDialogParentDies(t *testing.T) {
	cases := []struct {
		name string
		run  func(p *qtwidgets.QWidget)
	}{
		{"Question", func(p *qtwidgets.QWidget) { checkNoButton(t, qtwidgets.QMessageBoxQuestion(p, "t", "x")) }},
		{"Information", func(p *qtwidgets.QWidget) { checkNoButton(t, qtwidgets.QMessageBoxInformation(p, "t", "x")) }},
		{"Warning", func(p *qtwidgets.QWidget) { checkNoButton(t, qtwidgets.QMessageBoxWarning(p, "t", "x")) }},
		{"Critical", func(p *qtwidgets.QWidget) { checkNoButton(t, qtwidgets.QMessageBoxCritical(p, "t", "x")) }},
		{"QuestionFull", func(p *qtwidgets.QWidget) {
			checkNoButton(t, qtwidgets.QMessageBoxQuestionFull(p, "t", "x", qtwidgets.QMessageBoxYes|qtwidgets.QMessageBoxNo, qtwidgets.QMessageBoxNo))
		}},
		{"About", func(p *qtwidgets.QWidget) { qtwidgets.QMessageBoxAbout(p, "t", "x") }},
		{"AboutQt", func(p *qtwidgets.QWidget) { qtwidgets.QMessageBoxAboutQt(p) }},
		{"Color", func(p *qtwidgets.QWidget) {
			qtwidgets.QColorDialogGetColorFull(qtgui.RGB(1, 2, 3), p, "t", qtwidgets.QColorDialogDontUseNativeDialog)
		}},
		{"Font", func(p *qtwidgets.QWidget) { qtwidgets.QFontDialogGetFontFull(p) }},
		{"Text", func(p *qtwidgets.QWidget) { qtwidgets.QInputDialogGetText(p, "t", "x") }},
		{"File", func(p *qtwidgets.QWidget) {
			qtwidgets.QFileDialogGetOpenFileNameFull(p, "t", t.TempDir(), "", "", qtwidgets.QFileDialogDontUseNativeDialog)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			qutetest.Run(t, func() {
				p := qtwidgets.NewQWidget(nil)
				p.Show()
				timer := qtcore.NewQTimer(nil)
				timer.SetInterval(10)
				timer.OnTimeout(func() {
					if qtwidgets.QApplicationActiveModalWidget() != nil && p.Alive() {
						p.DeleteLater()
					}
				})
				timer.Start()
				c.run(p)
				timer.Delete()
				qutetest.Wait(10)
				if p.Alive() {
					t.Error("the parent outlived the test")
					p.Delete()
				}
			})
		})
	}
}

func checkNoButton(t *testing.T, got qtwidgets.QMessageBoxStandardButtons) {
	t.Helper()
	if got != qtwidgets.QMessageBoxNoButton {
		t.Errorf("returned %v when its parent died, want NoButton", got)
	}
}

// The heap-built message box still answers as Qt's does: the button clicked,
// and the default chosen from the buttons.
func TestMessageBoxStaticAnswers(t *testing.T) {
	qutetest.Run(t, func() {
		timer := answerModal(t, func(w *qtwidgets.QWidget) {
			box := qtwidgets.QMessageBoxCast(w)
			if d := box.DefaultButton(); d == nil || box.StandardButton(d.AsQAbstractButton()) != qtwidgets.QMessageBoxNo {
				t.Errorf("default button is not No")
			}
			box.Button(qtwidgets.QMessageBoxYes).Click()
		})
		got := qtwidgets.QMessageBoxQuestionFull(nil, "t", "x", qtwidgets.QMessageBoxYes|qtwidgets.QMessageBoxNo, qtwidgets.QMessageBoxNo)
		timer.Delete()
		if got != qtwidgets.QMessageBoxYes {
			t.Errorf("QMessageBoxQuestionFull = %v, want Yes", got)
		}
		timer = answerModal(t, func(w *qtwidgets.QWidget) {
			box := qtwidgets.QMessageBoxCast(w)
			if d := box.DefaultButton(); d == nil || box.StandardButton(d.AsQAbstractButton()) != qtwidgets.QMessageBoxOk {
				t.Errorf("default button is not Ok")
			}
			qutetest.Key(w, qtcore.Key_Return, 0)
		})
		got = qtwidgets.QMessageBoxWarning(nil, "t", "x")
		timer.Delete()
		if got != qtwidgets.QMessageBoxOk {
			t.Errorf("QMessageBoxWarning = %v, want Ok", got)
		}
	})
}

// ExecButton returns the standard button clicked, typed; NoButton for a
// button of the box's own, and when the box died with its parent.
func TestMessageBoxExecButton(t *testing.T) {
	qutetest.Run(t, func() {
		box := qtwidgets.NewQMessageBoxWithIcon(qtwidgets.QMessageBoxIconQuestion, "t", "x", qtwidgets.QMessageBoxYes|qtwidgets.QMessageBoxNo, nil)
		defer box.Delete()
		own := box.AddButtonText("Mine", qtwidgets.QMessageBoxActionRole)
		qutetest.WhenModal(t, func(*qtwidgets.QWidget) { box.Button(qtwidgets.QMessageBoxNo).Click() })
		if got := box.ExecButton(); got != qtwidgets.QMessageBoxNo {
			t.Errorf("ExecButton after clicking No = %v", got)
		}
		qutetest.WhenModal(t, func(*qtwidgets.QWidget) { own.Click() })
		if got := box.ExecButton(); got != qtwidgets.QMessageBoxNoButton {
			t.Errorf("ExecButton after clicking a button of its own = %v, want NoButton", got)
		}

		p := qtwidgets.NewQWidget(nil)
		p.Show()
		child := qtwidgets.NewQMessageBoxWithIcon(qtwidgets.QMessageBoxIconWarning, "t", "x", qtwidgets.QMessageBoxOk, p)
		qutetest.WhenModal(t, func(*qtwidgets.QWidget) { p.DeleteLater() })
		if got := child.ExecButton(); got != qtwidgets.QMessageBoxNoButton || child.Alive() {
			t.Errorf("ExecButton of a box whose parent died = %v (alive %v), want NoButton", got, child.Alive())
		}
	})
}
