// Hand-written; goqtgen never overwrites *_manual files.
#include "application_manual.h"

#include <QtCore/QEvent>
#include <QtCore/QObject>
#include <QtWidgets/QApplication>
#include <QtWidgets/QWidget>

#include <cstring>

extern "C" void goqt_qtwidgets_visibility(void *obj, bool visible);

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
            goqt_qtwidgets_visibility(static_cast<void *>(o), t == QEvent::Show);
        }
        return false;
    }
};

int goqt_argc = 0;
char **goqt_argv = nullptr;
} // namespace

extern "C" void *goqt_application_new(const char *args, const ptrdiff_t *lens, ptrdiff_t n) {
    // QApplication keeps references to argc and argv for its whole life.
    goqt_argc = int(n);
    goqt_argv = new char *[n + 1];
    for (ptrdiff_t i = 0; i < n; i++) {
        goqt_argv[i] = new char[lens[i] + 1];
        std::memcpy(goqt_argv[i], args, lens[i]);
        goqt_argv[i][lens[i]] = 0;
        args += lens[i];
    }
    goqt_argv[n] = nullptr;
    auto app = new QApplication(goqt_argc, goqt_argv);
    app->installEventFilter(new VisibilityFilter(app));
    return static_cast<QApplication *>(app);
}
