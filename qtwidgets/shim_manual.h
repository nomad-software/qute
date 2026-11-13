// Hand-written helpers that cpp overlays in spec/overlays/qtwidgets.yaml
// call. qtwidgets_gen.cpp includes this file after Qt's headers.
#ifndef QUTE_QTWIDGETS_SHIM_MANUAL_H
#define QUTE_QTWIDGETS_SHIM_MANUAL_H

#include <QDialogButtonBox>
#include <QMessageBox>
#include <QPointer>
#include <QPushButton>

// qute_message_box is QMessageBox::question, information, warning and
// critical with the box on the heap. Qt builds it on the stack with parent as
// its Qt parent, so if the parent is deleted while the box is open (a handler
// of the nested event loop deletes it) the box is destroyed twice. Here the
// parent frees it once, and NoButton is returned. The buttons and default are
// chosen as Qt's showNewMessageBox does.
inline QMessageBox::StandardButton qute_message_box(QMessageBox::Icon icon, QWidget *parent, const QString &title,
                                                    const QString &text, QMessageBox::StandardButtons buttons,
                                                    QMessageBox::StandardButton defaultButton) {
    QPointer<QMessageBox> box = new QMessageBox(icon, title, text, QMessageBox::NoButton, parent);
    QDialogButtonBox *bb = box->findChild<QDialogButtonBox *>();
    for (uint mask = QMessageBox::FirstButton; mask <= QMessageBox::LastButton; mask <<= 1) {
        uint sb = uint(buttons.toInt()) & mask;
        if (!sb) {
            continue;
        }
        QPushButton *b = box->addButton(QMessageBox::StandardButton(sb));
        if (box->defaultButton()) {
            continue;
        }
        if ((defaultButton == QMessageBox::NoButton && bb && bb->buttonRole(b) == QDialogButtonBox::AcceptRole) ||
            (defaultButton != QMessageBox::NoButton && sb == uint(defaultButton))) {
            box->setDefaultButton(b);
        }
    }
    int r = box->exec();
    if (box.isNull()) {
        return QMessageBox::NoButton;
    }
    QMessageBox::StandardButton res = r == -1 ? QMessageBox::Cancel : box->standardButton(box->clickedButton());
    delete box.data();
    return res;
}

// The short forms take Qt's defaults: Yes|No for a question, Ok otherwise.
inline QMessageBox::StandardButton qute_message_box(QMessageBox::Icon icon, QWidget *parent, const QString &title,
                                                    const QString &text) {
    QMessageBox::StandardButtons b = icon == QMessageBox::Question
                                         ? QMessageBox::StandardButtons(QMessageBox::Yes | QMessageBox::No)
                                         : QMessageBox::StandardButtons(QMessageBox::Ok);
    return qute_message_box(icon, parent, title, text, b, QMessageBox::NoButton);
}

#endif
