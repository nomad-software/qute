// Hand-written; qutegen never overwrites *_manual files.
#include "application_manual.h"

#include <QtCore/QEvent>
#include <QtCore/QObject>
#include <QtWidgets/QApplication>
#include <QtWidgets/QWidget>

#include <cstring>

extern "C" void qute_qtwidgets_visibility(void *obj, bool visible);

namespace {
// Pins Owned top-level windows while they are visible, so GC never deletes a
// window the user is looking at.
class VisibilityFilter : public QObject {
public:
    using QObject::QObject;
    bool eventFilter(QObject *o, QEvent *e) override {
        const auto t = e->type();
        if ((t == QEvent::Show || t == QEvent::Hide) && o->isWidgetType() &&
            static_cast<QWidget *>(o)->isWindow()) {
            qute_qtwidgets_visibility(static_cast<void *>(o), t == QEvent::Show);
        }
        return false;
    }
};

int qute_argc = 0;
char **qute_argv = nullptr;
} // namespace

extern "C" void *qute_application_new(const char *args, const ptrdiff_t *lens, ptrdiff_t n) {
    // QApplication keeps references to argc and argv for its whole life.
    qute_argc = int(n);
    qute_argv = new char *[n + 1];
    for (ptrdiff_t i = 0; i < n; i++) {
        qute_argv[i] = new char[lens[i] + 1];
        std::memcpy(qute_argv[i], args, lens[i]);
        qute_argv[i][lens[i]] = 0;
        args += lens[i];
    }
    qute_argv[n] = nullptr;
    auto app = new QApplication(qute_argc, qute_argv);
    app->installEventFilter(new VisibilityFilter(app));
    return static_cast<QApplication *>(app);
}
