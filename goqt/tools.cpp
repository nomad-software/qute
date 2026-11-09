#include "tools.h"

#include <QtCore/QCoreApplication>
#include <QtCore/QEventLoop>
#include <QtGui/QImage>
#include <QtGui/QPixmap>
#include <QtTest/QTest>
#include <QtWidgets/QLayout>
#include <QtWidgets/QWidget>

#include <cstring>

static char *dup(const QByteArray &b) {
    char *p = static_cast<char *>(std::malloc(b.size() + 1));
    std::memcpy(p, b.constData(), b.size());
    p[b.size()] = 0;
    return p;
}

extern "C" goqt_rgba goqt_tools_grab(void *widget) {
    QWidget *w = static_cast<QWidget *>(widget);
    // A widget that was never shown has not been laid out yet.
    w->ensurePolished();
    if (!w->isVisible()) {
        if (QLayout *l = w->layout()) l->activate();
        if (!w->testAttribute(Qt::WA_Resized)) w->adjustSize();
    }
    QImage img = w->grab().toImage().convertToFormat(QImage::Format_RGBA8888);
    goqt_rgba out{nullptr, img.width(), img.height()};
    const int row = img.width() * 4;
    out.data = static_cast<unsigned char *>(std::malloc(size_t(row) * img.height() + 1));
    for (int y = 0; y < img.height(); y++) {
        std::memcpy(out.data + size_t(y) * row, img.constScanLine(y), row);
    }
    return out;
}

extern "C" void goqt_tools_info(void *obj, goqt_objinfo *out) {
    QObject *o = static_cast<QObject *>(obj);
    out->cls = dup(QByteArray(o->metaObject()->className()));
    out->name = dup(o->objectName().toUtf8());
    out->widget = o->isWidgetType();
    out->visible = false;
    out->window = false;
    out->x = out->y = out->w = out->h = 0;
    if (out->widget) {
        QWidget *w = static_cast<QWidget *>(o);
        out->visible = w->isVisible();
        out->window = w->isWindow();
        QRect g = w->geometry();
        out->x = g.x();
        out->y = g.y();
        out->w = g.width();
        out->h = g.height();
    }
    const QObjectList &ch = o->children();
    out->nchildren = int(ch.size());
    out->children = static_cast<void **>(std::malloc(sizeof(void *) * (ch.size() + 1)));
    for (int i = 0; i < ch.size(); i++) out->children[i] = ch[i];
}

extern "C" void goqt_tools_free_info(goqt_objinfo *info) {
    std::free(info->cls);
    std::free(info->name);
    std::free(info->children);
}

extern "C" void goqt_tools_click(void *widget, int button, int x, int y, bool at) {
    QWidget *w = static_cast<QWidget *>(widget);
    QPoint pos = at ? QPoint(x, y) : QPoint();
    QTest::mouseClick(w, Qt::MouseButton(button), Qt::NoModifier, pos);
}

extern "C" void goqt_tools_dblclick(void *widget, int button) {
    QTest::mouseDClick(static_cast<QWidget *>(widget), Qt::MouseButton(button));
}

extern "C" void goqt_tools_type(void *widget, const char *s, ptrdiff_t n) {
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

extern "C" void goqt_tools_key(void *widget, int key, int mods) {
    QTest::keyClick(static_cast<QWidget *>(widget), Qt::Key(key), Qt::KeyboardModifiers::fromInt(mods));
}

extern "C" void goqt_tools_wait(int ms) { QTest::qWait(ms); }

extern "C" void goqt_tools_process(void) { QCoreApplication::processEvents(QEventLoop::AllEvents); }
