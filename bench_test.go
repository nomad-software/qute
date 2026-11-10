package qute_test

import (
	"fmt"
	"testing"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qutetest"
)

// The application event filter re-anchors a subtree's Go state on every
// reparenting, walking the subtree. These measure that walk: moving a
// populated subtree between two roots, and building a tree by adding
// populated subtrees, at a few sizes.
func BenchmarkReparentSubtree(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			qutetest.Run(b, func() {
				a, c := qtcore.NewQObject(nil), qtcore.NewQObject(nil)
				sub := qtcore.NewQObject(a)
				for i := 0; i < n; i++ {
					o := qtcore.NewQObject(sub)
					o.OnObjectNameChanged(func(string) {}) // anchored Go state
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if i%2 == 0 {
						sub.SetParent(c)
					} else {
						sub.SetParent(a)
					}
				}
				b.StopTimer()
				a.Delete()
				c.Delete()
			})
		})
	}
}

func BenchmarkBuildTreeFromSubtrees(b *testing.B) {
	for _, n := range []int{10, 100} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			qutetest.Run(b, func() {
				for i := 0; i < b.N; i++ {
					root := qtcore.NewQObject(nil)
					for j := 0; j < n; j++ {
						sub := qtcore.NewQObject(nil)
						for k := 0; k < 10; k++ {
							qtcore.NewQObject(sub).OnObjectNameChanged(func(string) {})
						}
						sub.SetParent(root)
					}
					root.Delete()
				}
			})
		})
	}
}
