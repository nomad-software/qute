package qtwebenginecore_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwebenginecore"
	"github.com/nomad-software/qute/qutetest"
)

// A builder's setters return the builder itself, for chaining.
func TestProfileBuilderChains(t *testing.T) {
	qutetest.Run(t, func() {
		b := qtwebenginecore.NewQWebEngineProfileBuilder()
		if got := b.SetCachePath(t.TempDir()).SetHttpCacheType(qtwebenginecore.QWebEngineProfileMemoryHttpCache); got != b {
			t.Error("a chained setter returned a different wrapper")
		}
		b.Delete()
	})
}

// A profile built with a parent is freed by it: Parented, not Owned.
func TestProfileWithParentIsParented(t *testing.T) {
	qutetest.Run(t, func() {
		parent := qtcore.NewQObject(nil)
		b := qtwebenginecore.NewQWebEngineProfileBuilder()
		prof := b.CreateProfileFull("", parent)
		if prof.Ownership() != qtcore.Parented {
			t.Errorf("profile with a parent: %v, want Parented", prof.Ownership())
		}
		b.Delete()
		parent.Delete()
		if prof.Alive() {
			t.Error("profile alive after its parent was deleted")
		}
	})
}

// Qt calls the cookie filter on a network thread. The hand-written binding
// hands it plain values, so the filter runs there without touching Qt, and
// what it returns decides whether the page may store and read the cookie.
func TestCookieFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "flavour", Value: "oat"})
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>cookies</body></html>"))
	}))
	defer srv.Close()

	for _, allow := range []bool{true, false} {
		var mu sync.Mutex
		var seen []qtwebenginecore.CookieFilterRequest
		var calls atomic.Int32
		cookie, loaded, done := "", false, false
		qutetest.Run(t, func() {
			prof := qtwebenginecore.NewQWebEngineProfile(nil)
			store := prof.CookieStore()
			store.SetCookieFilter(func(r qtwebenginecore.CookieFilterRequest) bool {
				calls.Add(1)
				mu.Lock()
				seen = append(seen, r)
				mu.Unlock()
				return allow
			})
			page := qtwebenginecore.NewQWebEnginePageWithProfile(prof, nil)
			page.OnLoadFinished(func(bool) { loaded = true })
			page.SetUrl(qtcore.NewQUrlWithUrl(srv.URL + "/"))
			for i := 0; i < 250 && !loaded; i++ {
				qutetest.Wait(20)
			}
			page.RunJavaScriptWithFunc("document.cookie", func(v *qtcore.QVariant) {
				cookie, _ = v.Value().(string)
				done = true
			})
			for i := 0; i < 100 && !done; i++ {
				qutetest.Wait(20)
			}
			store.SetCookieFilter(nil)
			page.Delete()
			prof.Delete()
		})
		if !loaded || !done {
			t.Fatalf("allow=%v: page loaded=%v, script ran=%v", allow, loaded, done)
		}
		if calls.Load() == 0 {
			t.Fatalf("allow=%v: the cookie filter was never called", allow)
		}
		mu.Lock()
		for _, r := range seen {
			if r.FirstPartyURL == "" || r.Origin == "" || r.ThirdParty {
				t.Errorf("allow=%v: filter request %+v, want a first-party request with both URLs", allow, r)
			}
		}
		mu.Unlock()
		if want := map[bool]string{true: "flavour=oat", false: ""}[allow]; cookie != want {
			t.Errorf("allow=%v: document.cookie = %q, want %q", allow, cookie, want)
		}
	}
}
