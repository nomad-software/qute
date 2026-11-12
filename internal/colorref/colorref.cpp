#include "colorref.h"
#include <QColor>

static int32_t pack(const QColor &c) {
	if (!c.isValid())
		return 0;
	return int32_t((uint32_t(c.red()) << 24) | (uint32_t(c.green()) << 16) | (uint32_t(c.blue()) << 8) | uint32_t(c.alpha()));
}

void qute_colorref_ops(const uint8_t *rgba, int n, int lf, int df, int32_t *ints, float *floats) {
	for (int i = 0; i < n; i++) {
		QColor c(rgba[4 * i], rgba[4 * i + 1], rgba[4 * i + 2], rgba[4 * i + 3]);
		int32_t *o = ints + 8 * i;
		c.getHsv(&o[0], &o[1], &o[2]);
		c.getHsl(&o[3], &o[4], &o[5]);
		o[6] = pack(c.lighter(lf));
		o[7] = pack(c.darker(df));
		float *f = floats + 6 * i;
		c.getHsvF(&f[0], &f[1], &f[2]);
		c.getHslF(&f[3], &f[4], &f[5]);
	}
}

void qute_colorref_from_ints(const int32_t *in, int n, int32_t *hsv, int32_t *hsl) {
	for (int i = 0; i < n; i++) {
		const int32_t *q = in + 4 * i;
		hsv[i] = pack(QColor::fromHsv(q[0], q[1], q[2], q[3]));
		hsl[i] = pack(QColor::fromHsl(q[0], q[1], q[2], q[3]));
	}
}

void qute_colorref_from_floats(const float *in, int n, int32_t *hsv, int32_t *hsl, int32_t *rgb, int32_t *alpha) {
	for (int i = 0; i < n; i++) {
		const float *q = in + 4 * i;
		hsv[i] = pack(QColor::fromHsvF(q[0], q[1], q[2], q[3]));
		hsl[i] = pack(QColor::fromHslF(q[0], q[1], q[2], q[3]));
		rgb[i] = pack(QColor::fromRgbF(q[0], q[1], q[2], q[3]));
		QColor w(Qt::white);
		w.setAlphaF(q[0]);
		alpha[i] = pack(w);
	}
}
