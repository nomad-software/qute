package qtwebenginecore_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// loadWithCookieFilter loads a page from a local server that sets a cookie,
// with filter installed on a fresh profile, and returns the page's
// document.cookie. It fails t if the page never loads.
func loadWithCookieFilter(t *testing.T, filter func(qtwebenginecore.CookieFilterRequest) bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "flavour", Value: "oat"})
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>cookies</body></html>"))
	}))
	defer srv.Close()
	cookie, loaded, done := "", false, false
	qutetest.Run(t, func() {
		prof := qtwebenginecore.NewQWebEngineProfile(nil)
		store := prof.CookieStore()
		store.SetCookieFilter(filter)
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
		t.Fatalf("page loaded=%v, script ran=%v", loaded, done)
	}
	return cookie
}

// Qt calls the cookie filter on a network thread. The hand-written binding
// hands it plain values, so the filter runs there without touching Qt, and
// what it returns decides whether the page may store and read the cookie.
func TestCookieFilter(t *testing.T) {
	for _, allow := range []bool{true, false} {
		var mu sync.Mutex
		var seen []qtwebenginecore.CookieFilterRequest
		cookie := loadWithCookieFilter(t, func(r qtwebenginecore.CookieFilterRequest) bool {
			mu.Lock()
			seen = append(seen, r)
			mu.Unlock()
			return allow
		})
		mu.Lock()
		if len(seen) == 0 {
			t.Errorf("allow=%v: the cookie filter was never called", allow)
		}
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

// Qt requires a profile to outlive its pages, so deleting a profile a live
// page uses panics; once the page is gone it is allowed.
func TestDeleteProfileUnderPagePanics(t *testing.T) {
	qutetest.Run(t, func() {
		prof := qtwebenginecore.NewQWebEngineProfile(nil)
		page := qtwebenginecore.NewQWebEnginePageWithProfile(prof, nil)
		var msg string
		func() {
			defer func() { msg, _ = recover().(string) }()
			prof.Delete()
		}()
		if !strings.Contains(msg, "QWebEnginePage still uses this QWebEngineProfile") {
			t.Errorf("Delete of a page's profile: panic %q", msg)
		}
		page.Delete()
		prof.Delete()
		if prof.Alive() {
			t.Error("profile alive after Delete")
		}
	})
}

// A panic in the cookie filter, which runs on Chromium's thread, must not
// end the process: it is printed and the cookie is blocked.
func TestCookieFilterPanicBlocks(t *testing.T) {
	var calls atomic.Int32
	cookie := loadWithCookieFilter(t, func(qtwebenginecore.CookieFilterRequest) bool {
		calls.Add(1)
		panic("filter failed")
	})
	if calls.Load() == 0 {
		t.Fatal("the cookie filter was never called")
	}
	if cookie != "" {
		t.Errorf("document.cookie = %q after the filter panicked, want the cookie blocked", cookie)
	}
}
