#include "input.h"

#include <QtCore/QString>
#include <QtTest/QTest>
#include <QtGui/QMouseEvent>
#include <QtGui/QPointingDevice>
#include <QtWidgets/QApplication>
#include <QtWidgets/QWidget>

extern "C" void qute_tools_click(void *widget, int button) {
    QTest::mouseClick(static_cast<QWidget *>(widget), Qt::MouseButton(button));
}

extern "C" void qute_tools_dblclick(void *widget, int button) {
    QTest::mouseDClick(static_cast<QWidget *>(widget), Qt::MouseButton(button));
}

extern "C" void qute_tools_type(void *widget, const char *s, ptrdiff_t n) {
    QWidget *w = static_cast<QWidget *>(widget);
    const QString text = QString::fromUtf8(s, n);
    // QTest::keyClicks only handles ASCII; send other characters as text with
    // an unknown key code, which is what an input method does.
    for (qsizetype i = 0; i < text.size(); i++) {
        QChar c = text.at(i);
        QString one(c);
        if (c.isHighSurrogate() && i + 1 < text.size()) {
            one.append(text.at(++i));
        }
        if (c.unicode() < 128) {
            QTest::keyClick(w, char(c.unicode()));
        } else {
            QTest::sendKeyEvent(QTest::Click, w, Qt::Key_unknown, one, Qt::NoModifier);
        }
    }
}

extern "C" void qute_tools_key(void *widget, int key, int mods) {
    QTest::keyClick(static_cast<QWidget *>(widget), Qt::Key(key), Qt::KeyboardModifiers::fromInt(mods));
}

extern "C" void qute_tools_wait(int ms) { QTest::qWait(ms); }

// The buttons held between qute_tools_mouse calls, so a move after a press
// carries the button, as a drag does.
static Qt::MouseButtons qute_held;

// action: 0 press, 1 move, 2 release. The events are built here rather than
// by QTest::mousePress and friends, which take QPoint(0, 0) to mean the
// widget's centre; like QTest they are marked spontaneous and go through
// QApplication::notify, so focus and the like follow as for a user.
extern "C" void qute_tools_mouse(void *widget, int action, int button, int mods, int x, int y) {
    QWidget *w = static_cast<QWidget *>(widget);
    const QPointF pos(x, y);
    const auto b = Qt::MouseButton(button);
    QEvent::Type type = QEvent::MouseMove;
    switch (action) {
    case 0:
        type = QEvent::MouseButtonPress;
        qute_held |= b;
        break;
    case 2:
        type = QEvent::MouseButtonRelease;
        qute_held &= ~Qt::MouseButtons(b);
        break;
    default:
        break;
    }
    QMouseEvent me(type, pos, w->mapToGlobal(pos), action == 1 ? Qt::NoButton : b, qute_held,
                   Qt::KeyboardModifiers::fromInt(mods), QPointingDevice::primaryPointingDevice());
    QSpontaneKeyEvent::setSpontaneous(&me);
    QApplication::instance()->notify(w, &me);
}
