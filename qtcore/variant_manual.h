// Hand-written; goqtgen never overwrites *_manual files.
#ifndef GOQT_VARIANT_MANUAL_H
#define GOQT_VARIANT_MANUAL_H
#include "goqt_common.h"
#ifdef __cplusplus
extern "C" {
#endif
void *goqt_variant_null(void);
void *goqt_variant_bool(bool v);
void *goqt_variant_int(int v);
void *goqt_variant_int64(long long v);
void *goqt_variant_uint64(unsigned long long v);
void *goqt_variant_double(double v);
void *goqt_variant_string(const char *d, ptrdiff_t n);
void *goqt_variant_bytes(const char *d, ptrdiff_t n);
void *goqt_variant_strlist(const char *d, const ptrdiff_t *lens, ptrdiff_t n);
void *goqt_variant_point(goqt_point p);
void *goqt_variant_pointf(goqt_pointf p);
void *goqt_variant_size(goqt_size s);
void *goqt_variant_rect(goqt_rect r);
int goqt_variant_kind(void *v);
bool goqt_variant_to_bool(void *v);
long long goqt_variant_to_int64(void *v);
unsigned long long goqt_variant_to_uint64(void *v);
double goqt_variant_to_double(void *v);
goqt_str goqt_variant_to_string(void *v);
goqt_str goqt_variant_to_bytes(void *v);
goqt_list goqt_variant_to_strlist(void *v);
goqt_point goqt_variant_to_point(void *v);
goqt_pointf goqt_variant_to_pointf(void *v);
goqt_size goqt_variant_to_size(void *v);
goqt_rect goqt_variant_to_rect(void *v);
#ifdef __cplusplus
}
#endif
#endif
