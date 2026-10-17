package catalog

import (
	"strings"
	"testing"
)

func TestHiddenBindingLeavesTheEndpointList(t *testing.T) {
	c := openTest(t)
	_, ownCred := seed(t, c, "https://own.example/v1", "sk-own", "own-a")
	_, keepCred := seed(t, c, "https://keep.example/v1", "sk-keep", "keep-a")
	_, hideCred := seed(t, c, "https://hide.example/v1", "sk-hide", "hide-a")

	own, err := c.AddBinding("pi", "own", ownCred.ID, "own-a", []string{"own-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("pi", "keep", keepCred.ID, "keep-a", []string{"keep-a"}); err != nil {
		t.Fatal(err)
	}
	hidden, err := c.AddBinding("pi", "hide", hideCred.ID, "hide-a", []string{"hide-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(own.ID); err != nil {
		t.Fatal(err)
	}

	spec, err := c.ProjectSpec(own)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Groups) != 2 {
		t.Fatalf("groups before hiding = %+v, want both other endpoints", spec.Groups)
	}

	if err := c.SetBindingHidden(hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	spec, err = c.ProjectSpec(own)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Groups) != 1 || spec.Groups[0].Endpoint != "https://keep.example/v1" {
		t.Fatalf("groups after hiding = %+v, want only keep", spec.Groups)
	}
	// The hint counts what /model will hold: the active endpoint plus the
	// groups that survive the filter.
	if endpoints, models, err := c.AggregateCounts("pi"); err != nil || endpoints != 2 || models != 2 {
		t.Fatalf("AggregateCounts(pi) = %d endpoints, %d models, err=%v; want 2, 2", endpoints, models, err)
	}

	if err := c.SetBindingHidden(hidden.ID, false); err != nil {
		t.Fatal(err)
	}
	spec, err = c.ProjectSpec(own)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Groups) != 2 {
		t.Fatalf("groups after showing again = %+v, want both other endpoints", spec.Groups)
	}
}

func TestHiddenDuplicateDoesNotShadowVisible(t *testing.T) {
	c := openTest(t)
	_, ownCred := seed(t, c, "https://own.example/v1", "sk-own", "own-a")
	shared, firstCred := seed(t, c, "https://shared.example/v1", "sk-first", "shared-a")
	secondCred, err := c.PutCredential(shared.ID, "sk-second")
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.AddBinding("pi", "first", firstCred.ID, "shared-a", []string{"shared-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBinding("pi", "second", secondCred.ID, "shared-a", []string{"shared-a"}); err != nil {
		t.Fatal(err)
	}
	own, err := c.AddBinding("pi", "own", ownCred.ID, "own-a", []string{"own-a"})
	if err != nil {
		t.Fatal(err)
	}

	// The hidden binding comes first in file order; the endpoint must still be
	// represented by the visible one rather than dropped.
	if err := c.SetBindingHidden(first.ID, true); err != nil {
		t.Fatal(err)
	}
	spec, err := c.ProjectSpec(own)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Groups) != 1 || spec.Groups[0].Key != "sk-second" {
		t.Fatalf("groups = %+v, want the visible binding's key", spec.Groups)
	}
}

func TestActiveBindingCannotBeHidden(t *testing.T) {
	c := openTest(t)
	_, cr := seed(t, c, "https://one.example/v1", "sk-one", "one")
	active, err := c.AddBinding("pi", "one", cr.ID, "one", []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(active.ID); err != nil {
		t.Fatal(err)
	}
	err = c.SetBindingHidden(active.ID, true)
	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("hiding the active binding = %v, want a refusal naming it active", err)
	}

	// A tool that holds one endpoint has no list to hide from.
	codex, err := c.AddBinding("codex", "cx", cr.ID, "one", []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetBindingHidden(codex.ID, true); err == nil {
		t.Fatal("hiding a codex binding succeeded, want a refusal")
	}
}

func TestSwitchingToHiddenBindingShowsIt(t *testing.T) {
	c := openTest(t)
	_, aCred := seed(t, c, "https://a.example/v1", "sk-a", "a")
	_, bCred := seed(t, c, "https://b.example/v1", "sk-b", "b")
	a, err := c.AddBinding("pi", "a", aCred.ID, "a", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.AddBinding("pi", "b", bCred.ID, "b", []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBindingHidden(b.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	bs, err := c.Bindings("pi")
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range bs {
		if got.ID != b.ID {
			continue
		}
		if got.Hidden {
			t.Fatal("switching to a hidden binding left it hidden")
		}
		return
	}
	t.Fatal("binding b disappeared")
}

func TestEditingKeepsHidden(t *testing.T) {
	c := openTest(t)
	_, cr := seed(t, c, "https://one.example/v1", "sk-one", "one", "two")
	keep, err := c.AddBinding("pi", "keep", cr.ID, "one", []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := c.AddBinding("pi", "other", cr.ID, "one", []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Activate(other.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBindingHidden(keep.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateBinding(keep.ID, "keep", cr.ID, "two", []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	bs, err := c.Bindings("pi")
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range bs {
		if got.ID == keep.ID && !got.Hidden {
			t.Fatal("editing a hidden binding made it visible again")
		}
	}
}
