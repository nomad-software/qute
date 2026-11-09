// C interface of the qute runtime shim. Only plain C types cross this line.
#ifndef QUTE_RT_H
#define QUTE_RT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void qute_rt_init(void);
bool qute_rt_is_gui_thread(void);
bool qute_rt_app_exists(void);
// Queue a call to qute_rt_run(id) on the GUI thread. Returns false if there
// is no application object to post to.
bool qute_rt_post(uintptr_t id);
// Queue one drain of the cleanup queue on the GUI thread.
bool qute_rt_post_drain(void);
// Connect QObject::destroyed of obj to qute_rt_destroyed(obj).
void qute_rt_watch(void *obj);
bool qute_rt_has_parent(void *obj);
void qute_rt_delete_qobject(void *obj);
void qute_rt_disconnect(void *conn);
void qute_rt_free_connection(void *conn);

#ifdef __cplusplus
}
#endif

#endif
