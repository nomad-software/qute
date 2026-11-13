// Hand-written; qutegen never overwrites *_manual.go files.

package qtwidgets

// ExecButton shows the box modally, as Exec does, and returns the standard
// button the user chose, typed: Exec is QDialog::exec, which returns an int,
// and for a box with buttons of its own that int is an opaque value. It
// returns NoButton when a button of the box's own (AddButtonText) was
// clicked, where ClickedButton says which, and when the box was destroyed
// while open (its parent was deleted).
func (this *QMessageBox) ExecButton() QMessageBoxStandardButtons {
	this.Exec()
	if !this.Alive() {
		return QMessageBoxNoButton
	}
	b := this.ClickedButton()
	if b == nil {
		return QMessageBoxNoButton
	}
	return this.StandardButton(b)
}
