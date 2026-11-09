// C interface of the goqt runtime shim. Only plain C types cross this line.
#ifndef GOQT_RT_H
#define GOQT_RT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void goqt_rt_init(void);
bool goqt_rt_is_gui_thread(void);
bool goqt_rt_app_exists(void);
// Queue a call to goqt_rt_run(id) on the GUI thread. Returns false if there
// is no application object to post to.
bool goqt_rt_post(uintptr_t id);
// Queue one drain of the cleanup queue on the GUI thread.
bool goqt_rt_post_drain(void);
// Connect QObject::destroyed of obj to goqt_rt_destroyed(obj).
void goqt_rt_watch(void *obj);
bool goqt_rt_has_parent(void *obj);
void goqt_rt_delete_qobject(void *obj);
void goqt_rt_disconnect(void *conn);
void goqt_rt_free_connection(void *conn);
const char *goqt_rt_class_name(void *obj);
void goqt_rt_free(void *p);

#ifdef __cplusplus
}
#endif

#endif
