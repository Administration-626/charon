//go:build linux || darwin

package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMutationWaitsForOtherCatalogInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c1 := openTest(t)
	c2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	_, firstCredential := seed(t, c1, "https://first.example/v1", "sk-first", "model-a")
	first, err := c1.AddBinding("claude", "first", firstCredential.ID, "model-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, laterCredential := seed(t, c1, "https://later.example/v1", "sk-later", "model-b")
	later, err := c1.AddBinding("claude", "later", laterCredential.ID, "model-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c1.Activate(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := c1.lock(); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := c2.Activate(later.ID)
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		c1.unlock()
		t.Fatalf("competing mutation returned before lock release: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	c1.unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("mutation after lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("competing mutation did not finish after lock release")
	}
	active, found, err := c1.Active("claude")
	if err != nil || !found || active.ID != later.ID {
		t.Fatalf("active = %+v, found=%v, err=%v; want later binding %s", active, found, err, later.ID)
	}
	settings, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(settings); !strings.Contains(got, "https://later.example") || !strings.Contains(got, "sk-later") {
		t.Fatalf("live config = %s; want later binding after waiter completed", settings)
	}
}
