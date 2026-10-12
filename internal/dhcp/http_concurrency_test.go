package dhcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type signaledBody struct {
	read   chan struct{}
	reader io.Reader
}

func (b *signaledBody) Read(p []byte) (int, error) {
	select {
	case <-b.read:
	default:
		close(b.read)
	}
	return b.reader.Read(p)
}
func (b *signaledBody) Close() error { return nil }

func TestPatchPreservesChangeMadeWhileWaitingForLock(t *testing.T) {
	h, a, lock := testHTTP(t)
	if w := do(t, h, http.MethodPut, "/config", validPUT); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	held, release, unlocked := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(unlocked)
		_ = lock.Do(context.Background(), func() error { close(held); <-release; return nil })
	}()
	<-held
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		<-unlocked
	}()
	read := make(chan struct{})
	r := httptest.NewRequest(http.MethodPatch, "/config?confirm=true", nil)
	r.Body = &signaledBody{read: read, reader: strings.NewReader(`{"enabled":false}`)}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, r); done <- w }()
	<-read // The body is read before queuing; intent must only be loaded under the lock.
	cfg, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExtraConf = "log-dhcp"
	doc, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc.Set(ModuleName, raw)
	if err := a.Store.Save(doc); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case w := <-done:
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("patch did not finish")
	}
	cfg, err = a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExtraConf != "log-dhcp" {
		t.Fatal("PATCH lost another writer's unrelated field")
	}

}
