#include "georef.h"
#include <QRect>
#include <QRectF>

void qute_georef_rect(const int32_t *r, const int32_t *p, const int32_t *o, int32_t *out) {
	QRect a(r[0], r[1], r[2], r[3]), b(o[0], o[1], o[2], o[3]);
	QPoint c = a.center();
	out[0] = c.x();
	out[1] = c.y();
	out[2] = a.contains(QPoint(p[0], p[1]));
	out[3] = a.intersects(b);
	out[4] = a.isEmpty();
}

void qute_georef_rectf(const double *r, const double *p, int32_t *out) {
	QRectF a(r[0], r[1], r[2], r[3]);
	QRect t = a.toRect();
	out[0] = t.x();
	out[1] = t.y();
	out[2] = t.width();
	out[3] = t.height();
	out[4] = a.contains(QPointF(p[0], p[1]));
}
