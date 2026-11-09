// Hand-written; goqtgen never overwrites *_manual.go files.

package qtcore

// #include <stdlib.h>
// #include "variant_manual.h"
import "C"

import (
	"fmt"
	"unsafe"

	rt "github.com/nomad-software/qute/internal/rt"
)

// NewQVariantOf converts a Go value to an Owned QVariant. It accepts nil,
// bool, int, int32, int64, uint, uint32, uint64, float32, float64, string,
// []byte, []string, *QVariant (copied) and the geometry structs.
func NewQVariantOf(v any) *QVariant {
	rt.CheckThread("NewQVariantOf")
	var p unsafe.Pointer
	switch x := v.(type) {
	case nil:
		p = C.goqt_variant_null()
	case bool:
		p = C.goqt_variant_bool(C.bool(x))
	case int:
		p = C.goqt_variant_int64(C.longlong(x))
	case int32:
		p = C.goqt_variant_int(C.int(x))
	case int64:
		p = C.goqt_variant_int64(C.longlong(x))
	case uint:
		p = C.goqt_variant_uint64(C.ulonglong(x))
	case uint32:
		p = C.goqt_variant_uint64(C.ulonglong(x))
	case uint64:
		p = C.goqt_variant_uint64(C.ulonglong(x))
	case float32:
		p = C.goqt_variant_double(C.double(x))
	case float64:
		p = C.goqt_variant_double(C.double(x))
	case string:
		p = C.goqt_variant_string((*C.char)(unsafe.Pointer(unsafe.StringData(x))), C.ptrdiff_t(len(x)))
	case []byte:
		p = C.goqt_variant_bytes((*C.char)(unsafe.Pointer(unsafe.SliceData(x))), C.ptrdiff_t(len(x)))
	case []string:
		d, l := goqtStrListArg(x)
		p = C.goqt_variant_strlist((*C.char)(unsafe.Pointer(unsafe.SliceData(d))), (*C.ptrdiff_t)(unsafe.SliceData(l)), C.ptrdiff_t(len(x)))
	case *QVariant:
		return NewQVariantOf(x.Value())
	case QPoint:
		p = C.goqt_variant_point(cQPoint(x))
	case QSize:
		p = C.goqt_variant_size(cQSize(x))
	case QRect:
		p = C.goqt_variant_rect(cQRect(x))
	case QPointF:
		p = C.goqt_variant_pointf(cQPointF(x))
	default:
		panic(fmt.Sprintf("NewQVariantOf: unsupported Go type %T; convert it to a basic type first", v))
	}
	return QVariantFromPtr(p, Owned)
}

// Value converts the variant to the closest Go value: nil, bool, int64,
// uint64, float64, string, []byte, []string or a geometry struct. Other types
// are returned as their string form, or nil if Qt cannot convert them.
func (this *QVariant) Value() any {
	self := rt.Use(unsafe.Pointer(this), "QVariant.Value")
	switch C.goqt_variant_kind(self) {
	case 0:
		return nil
	case 1:
		return bool(C.goqt_variant_to_bool(self))
	case 2:
		return int64(C.goqt_variant_to_int64(self))
	case 3:
		return uint64(C.goqt_variant_to_uint64(self))
	case 4:
		return float64(C.goqt_variant_to_double(self))
	case 5:
		return goqtStr(C.goqt_variant_to_string(self))
	case 6:
		return goqtBytes(C.goqt_variant_to_bytes(self))
	case 7:
		return goqtStrList(C.goqt_variant_to_strlist(self))
	case 8:
		return goQPoint(C.goqt_variant_to_point(self))
	case 9:
		return goQSize(C.goqt_variant_to_size(self))
	case 10:
		return goQRect(C.goqt_variant_to_rect(self))
	case 11:
		return goQPointF(C.goqt_variant_to_pointf(self))
	case 12:
		return goqtStr(C.goqt_variant_to_string(self))
	}
	return nil
}
