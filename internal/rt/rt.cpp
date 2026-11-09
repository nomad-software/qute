#include "rt.h"

#include <QtCore/QCoreApplication>
#include <QtCore/QMetaObject>
#include <QtCore/QObject>
#include <QtCore/QThread>

#include <thread>

extern "C" {
// Exported from Go (cgo.go).
void qute_rt_run(uintptr_t id);
void qute_rt_drain(void);
void qute_rt_destroyed(void *obj);
}

static std::thread::id qute_main_thread;

extern "C" void qute_rt_init(void) { qute_main_thread = std::this_thread::get_id(); }

extern "C" bool qute_rt_is_gui_thread(void) {
    QCoreApplication *app = QCoreApplication::instance();
    if (app) {
        return QThread::currentThread() == app->thread();
    }
    return std::this_thread::get_id() == qute_main_thread;
}

extern "C" bool qute_rt_app_exists(void) { return QCoreApplication::instance() != nullptr; }

extern "C" bool qute_rt_post(uintptr_t id) {
    QCoreApplication *app = QCoreApplication::instance();
    if (!app) {
        return false;
    }
    return QMetaObject::invokeMethod(app, [id] { qute_rt_run(id); }, Qt::QueuedConnection);
}

extern "C" bool qute_rt_post_drain(void) {
    QCoreApplication *app = QCoreApplication::instance();
    if (!app) {
        return false;
    }
    return QMetaObject::invokeMethod(app, [] { qute_rt_drain(); }, Qt::QueuedConnection);
}

extern "C" void qute_rt_watch(void *obj) {
    QObject *o = static_cast<QObject *>(obj);
    // No context object: the connection lives exactly as long as o does.
    QObject::connect(o, &QObject::destroyed, [o] { qute_rt_destroyed(o); });
}

extern "C" bool qute_rt_has_parent(void *obj) { return static_cast<QObject *>(obj)->parent() != nullptr; }

extern "C" void qute_rt_delete_qobject(void *obj) { delete static_cast<QObject *>(obj); }

extern "C" void qute_rt_disconnect(void *conn) {
    auto *c = static_cast<QMetaObject::Connection *>(conn);
    QObject::disconnect(*c);
}

extern "C" void qute_rt_free_connection(void *conn) { delete static_cast<QMetaObject::Connection *>(conn); }
