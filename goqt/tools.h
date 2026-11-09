#ifndef GOQT_TOOLS_H
#define GOQT_TOOLS_H
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#ifdef __cplusplus
extern "C" {
#endif
typedef struct { unsigned char *data; int w, h; } goqt_rgba;
typedef struct {
    char *cls;
    char *name;
    bool widget;
    bool visible;
    bool window;
    int x, y, w, h;
    void **children;
    int nchildren;
} goqt_objinfo;

goqt_rgba goqt_tools_grab(void *widget);
void goqt_tools_info(void *obj, goqt_objinfo *out);
void goqt_tools_free_info(goqt_objinfo *info);
void goqt_tools_click(void *widget, int button, int x, int y, bool at);
void goqt_tools_dblclick(void *widget, int button);
void goqt_tools_type(void *widget, const char *s, ptrdiff_t n);
void goqt_tools_key(void *widget, int key, int mods);
void goqt_tools_wait(int ms);
void goqt_tools_process(void);
#ifdef __cplusplus
}
#endif
#endif
