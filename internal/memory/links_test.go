package memory

import "testing"

func TestBacklinksFindsMemoriesPointingAtTheName(t *testing.T) {
	// The real shape (§I.5): `[[<filename stem>]]` inside another memory's body.
	pool := []Memory{
		{Name: "feedback_short_commit", Body: "Keep it terse per [[feedback_no_local_paths]]."},
		{Name: "feedback_no_local_paths", Body: "Scrub paths. See [[feedback_short_commit]]."},
		{Name: "project_roadmap", Body: "Rename to Beacon. No links here."},
	}
	got := Backlinks(pool, "feedback_no_local_paths")
	if len(got) != 1 {
		t.Fatalf("got %d backlinks, want 1: %+v", len(got), got)
	}
	if got[0].Name != "feedback_short_commit" {
		t.Errorf("backlink = %q, want feedback_short_commit", got[0].Name)
	}
}

func TestBacklinksIgnoresTheMemorysOwnSelfReference(t *testing.T) {
	pool := []Memory{
		{Name: "project_roadmap", Body: "See [[project_roadmap]] — a self link."},
	}
	if got := Backlinks(pool, "project_roadmap"); len(got) != 0 {
		t.Errorf("got %d backlinks, want 0 — a self link is not inbound", len(got))
	}
}
