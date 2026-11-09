// Hand-written; goqtgen never overwrites *_manual files.
#include "variant_manual.h"

#include <QtCore/QStringList>
#include <QtCore/QVariant>

static QVariant &V(void *v) { return *static_cast<QVariant *>(v); }

extern "C" {
void *goqt_variant_null(void) { return new QVariant(); }
void *goqt_variant_bool(bool v) { return new QVariant(v); }
void *goqt_variant_int(int v) { return new QVariant(v); }
void *goqt_variant_int64(long long v) {
    if (v >= INT_MIN && v <= INT_MAX) return new QVariant(int(v));
    return new QVariant(qlonglong(v));
}
void *goqt_variant_uint64(unsigned long long v) { return new QVariant(qulonglong(v)); }
void *goqt_variant_double(double v) { return new QVariant(v); }
void *goqt_variant_string(const char *d, ptrdiff_t n) { return new QVariant(QString::fromUtf8(d, n)); }
void *goqt_variant_bytes(const char *d, ptrdiff_t n) { return new QVariant(QByteArray(d, n)); }
void *goqt_variant_strlist(const char *d, const ptrdiff_t *lens, ptrdiff_t n) {
    return new QVariant(QStringList(goqt_qstrlist(d, lens, n)));
}
void *goqt_variant_point(goqt_point p) { return new QVariant(goqt_to_QPoint(p)); }
void *goqt_variant_pointf(goqt_pointf p) { return new QVariant(goqt_to_QPointF(p)); }
void *goqt_variant_size(goqt_size s) { return new QVariant(goqt_to_QSize(s)); }
void *goqt_variant_rect(goqt_rect r) { return new QVariant(goqt_to_QRect(r)); }

int goqt_variant_kind(void *v) {
    const QVariant &x = V(v);
    if (!x.isValid() || x.isNull()) return 0;
    switch (x.typeId()) {
    case QMetaType::Bool: return 1;
    case QMetaType::Int:
    case QMetaType::LongLong:
    case QMetaType::Short:
    case QMetaType::Long:
    case QMetaType::Char:
    case QMetaType::SChar: return 2;
    case QMetaType::UInt:
    case QMetaType::ULongLong:
    case QMetaType::UShort:
    case QMetaType::ULong:
    case QMetaType::UChar: return 3;
    case QMetaType::Double:
    case QMetaType::Float: return 4;
    case QMetaType::QString: return 5;
    case QMetaType::QByteArray: return 6;
    case QMetaType::QStringList: return 7;
    case QMetaType::QPoint: return 8;
    case QMetaType::QSize: return 9;
    case QMetaType::QRect: return 10;
    case QMetaType::QPointF: return 11;
    }
    if (x.canConvert<QString>()) return 12;
    return -1;
}
bool goqt_variant_to_bool(void *v) { return V(v).toBool(); }
long long goqt_variant_to_int64(void *v) { return V(v).toLongLong(); }
unsigned long long goqt_variant_to_uint64(void *v) { return V(v).toULongLong(); }
double goqt_variant_to_double(void *v) { return V(v).toDouble(); }
goqt_str goqt_variant_to_string(void *v) { return goqt_ret_str(V(v).toString()); }
goqt_str goqt_variant_to_bytes(void *v) { return goqt_ret_bytes(V(v).toByteArray()); }
goqt_list goqt_variant_to_strlist(void *v) { return goqt_ret_strlist(V(v).toStringList()); }
goqt_point goqt_variant_to_point(void *v) { return goqt_from_QPoint(V(v).toPoint()); }
goqt_pointf goqt_variant_to_pointf(void *v) { return goqt_from_QPointF(V(v).toPointF()); }
goqt_size goqt_variant_to_size(void *v) { return goqt_from_QSize(V(v).toSize()); }
goqt_rect goqt_variant_to_rect(void *v) { return goqt_from_QRect(V(v).toRect()); }
}
