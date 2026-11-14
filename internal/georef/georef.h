// Hand-written: Qt's own QRect and QRectF arithmetic, which qtcore's Go
// geometry methods are tested against. Only tests import this package.
#pragma once
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
// For QRect(r[0..3]) (x, y, w, h), QPoint(p[0..1]) and QRect(o[0..3]):
// out[0..1] center, out[2] contains(p), out[3] intersects(o), out[4] isEmpty.
void qute_georef_rect(const int32_t *r, const int32_t *p, const int32_t *o, int32_t *out);
// For QRectF(r[0..3]) and QPointF(p[0..1]): out[0..3] toRect, out[4]
// contains(p).
void qute_georef_rectf(const double *r, const double *p, int32_t *out);
#ifdef __cplusplus
}
#endif
