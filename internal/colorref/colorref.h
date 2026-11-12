// Hand-written: Qt's own QColor arithmetic, which qtgui's Go colour
// methods are tested against. Only tests import this package.
#pragma once
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
// For n colours (rgba bytes): hsv h,s,v; hsl h,s,l; lighter(lf); darker(df)
// as packed 0xRRGGBBAA; 8 ints per colour. Floats: hsvF h,s,v; hslF h,s,l;
// 6 per colour.
void qute_colorref_ops(const uint8_t *rgba, int n, int lf, int df, int32_t *ints, float *floats);
// fromHsv(h,s,v,a) and fromHsl(h,s,l,a) for n int quads, packed.
void qute_colorref_from_ints(const int32_t *in, int n, int32_t *hsv, int32_t *hsl);
// fromHsvF, fromHslF, fromRgbF and an opaque white with setAlphaF(in[0])
// for n float quads, packed.
void qute_colorref_from_floats(const float *in, int n, int32_t *hsv, int32_t *hsl, int32_t *rgb, int32_t *alpha);
#ifdef __cplusplus
}
#endif
