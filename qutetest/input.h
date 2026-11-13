#ifndef QUTE_TEST_INPUT_H
#define QUTE_TEST_INPUT_H
#include <stdbool.h>
#include <stddef.h>
#ifdef __cplusplus
extern "C" {
#endif
void qute_tools_click(void *widget, int button);
void qute_tools_dblclick(void *widget, int button);
void qute_tools_type(void *widget, const char *s, ptrdiff_t n);
void qute_tools_key(void *widget, int key, int mods);
void qute_tools_wait(int ms);
void qute_tools_mouse(void *widget, int action, int button, int mods, int x, int y);
#ifdef __cplusplus
}
#endif
#endif
