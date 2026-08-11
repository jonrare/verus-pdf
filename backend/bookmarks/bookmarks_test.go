package bookmarks

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"

	"veruspdf/backend/internal/pdftest"
)

// ── Tree conversion ──────────────────────────────────────────────────────────

func TestConvert_FlattensToFrontendShape(t *testing.T) {
	in := []pdfcpu.Bookmark{
		{Title: "Chapter 1", PageFrom: 1, Bold: true},
		{Title: "Chapter 2", PageFrom: 5, Italic: true},
	}
	got := convert(in)

	if len(got) != 2 {
		t.Fatalf("got %d bookmarks, want 2", len(got))
	}
	if got[0].Title != "Chapter 1" || got[0].Page != 1 || !got[0].Bold {
		t.Errorf("first bookmark = %+v", got[0])
	}
	if got[1].Title != "Chapter 2" || got[1].Page != 5 || !got[1].Italic {
		t.Errorf("second bookmark = %+v", got[1])
	}
}

func TestConvert_PreservesNesting(t *testing.T) {
	in := []pdfcpu.Bookmark{{
		Title: "Part I", PageFrom: 1,
		Kids: []pdfcpu.Bookmark{
			{Title: "Chapter 1", PageFrom: 2},
			{Title: "Chapter 2", PageFrom: 8, Kids: []pdfcpu.Bookmark{
				{Title: "Section 2.1", PageFrom: 9},
			}},
		},
	}}
	got := convert(in)

	if len(got) != 1 || len(got[0].Kids) != 2 {
		t.Fatalf("top level = %+v", got)
	}
	if len(got[0].Kids[1].Kids) != 1 {
		t.Fatalf("second-level kids = %+v", got[0].Kids[1])
	}
	if got[0].Kids[1].Kids[0].Title != "Section 2.1" {
		t.Errorf("deepest title = %q, want %q", got[0].Kids[1].Kids[0].Title, "Section 2.1")
	}
}

func TestConvert_Empty(t *testing.T) {
	if got := convert(nil); got == nil || len(got) != 0 {
		t.Errorf("got %+v, want a non-nil empty slice so it marshals as [] not null", got)
	}
}

// sanitizeBookmarks exists to drop the internal state that api.Bookmarks
// populates (Parent pointers, AbsPos, Color) and that fails validation on the
// way back in. It must keep the fields that carry meaning.
func TestSanitizeBookmarks_KeepsMeaningfulFields(t *testing.T) {
	in := []pdfcpu.Bookmark{{
		Title: "Chapter 1", PageFrom: 3, Bold: true, Italic: true,
		Kids: []pdfcpu.Bookmark{{Title: "Section", PageFrom: 4}},
	}}
	got := sanitizeBookmarks(in)

	if len(got) != 1 {
		t.Fatalf("got %d bookmarks, want 1", len(got))
	}
	b := got[0]
	if b.Title != "Chapter 1" || b.PageFrom != 3 || !b.Bold || !b.Italic {
		t.Errorf("sanitised bookmark lost data: %+v", b)
	}
	if len(b.Kids) != 1 || b.Kids[0].Title != "Section" {
		t.Errorf("kids were not carried through: %+v", b.Kids)
	}
}

func TestSanitizeBookmarks_DropsParentPointers(t *testing.T) {
	parent := &pdfcpu.Bookmark{Title: "Parent"}
	in := []pdfcpu.Bookmark{{Title: "Child", PageFrom: 2, Parent: parent}}

	got := sanitizeBookmarks(in)
	if len(got) != 1 {
		t.Fatalf("got %d bookmarks, want 1", len(got))
	}
	if got[0].Parent != nil {
		t.Error("Parent pointer survived sanitisation, which breaks re-validation")
	}
}

func TestSanitizeBookmarks_Empty(t *testing.T) {
	if got := sanitizeBookmarks(nil); len(got) != 0 {
		t.Errorf("got %+v, want empty", got)
	}
}

// ── End to end ───────────────────────────────────────────────────────────────

func threePager(t *testing.T) string {
	t.Helper()
	return pdftest.Pages(t, "doc.pdf",
		"BT /F1 12 Tf 72 700 Td (One) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Two) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Three) Tj ET")
}

// A document with no outline is not an error — it just has no bookmarks.
func TestListBookmarks_NoOutline(t *testing.T) {
	got, err := (&Service{}).ListBookmarks(threePager(t))
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d bookmarks, want 0", len(got))
	}
}

func TestListBookmarks_MissingFile(t *testing.T) {
	if _, err := (&Service{}).ListBookmarks("/nonexistent/nope.pdf"); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestAddBookmark_ThenList(t *testing.T) {
	src := threePager(t)
	out := filepath.Join(t.TempDir(), "out.pdf")
	svc := &Service{}

	if res := svc.AddBookmark(src, out, "Introduction", 1); res.Error != "" {
		t.Fatalf("AddBookmark: %s", res.Error)
	}

	got, err := svc.ListBookmarks(out)
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d bookmarks, want 1: %+v", len(got), got)
	}
	if got[0].Title != "Introduction" || got[0].Page != 1 {
		t.Errorf("got %+v, want Introduction on page 1", got[0])
	}
}

func TestAddBookmark_PreservesExisting(t *testing.T) {
	src := threePager(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	svc := &Service{}

	if res := svc.AddBookmark(src, first, "First", 1); res.Error != "" {
		t.Fatalf("AddBookmark(First): %s", res.Error)
	}
	if res := svc.AddBookmark(first, second, "Second", 2); res.Error != "" {
		t.Fatalf("AddBookmark(Second): %s", res.Error)
	}

	got, err := svc.ListBookmarks(second)
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d bookmarks, want 2: %+v", len(got), got)
	}
	titles := map[string]bool{got[0].Title: true, got[1].Title: true}
	if !titles["First"] || !titles["Second"] {
		t.Errorf("got %+v, want both First and Second", got)
	}
}

func TestAddBookmark_Validation(t *testing.T) {
	src := threePager(t)
	out := filepath.Join(t.TempDir(), "out.pdf")
	svc := &Service{}

	if res := svc.AddBookmark(src, out, "", 1); res.Error == "" {
		t.Error("an empty title was accepted")
	}
	for _, page := range []int{0, -1} {
		if res := svc.AddBookmark(src, out, "Title", page); res.Error == "" {
			t.Errorf("page %d was accepted", page)
		}
	}
}

// The debug string reaches the UI and gets embedded in error messages, so it
// must not carry full filesystem paths.
func TestAddBookmark_DebugHasNoFullPaths(t *testing.T) {
	src := threePager(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	res := (&Service{}).AddBookmark(src, out, "Introduction", 1)
	if res.Debug == "" {
		t.Skip("no debug string produced")
	}
	if strings.Contains(res.Debug, filepath.Dir(src)) {
		t.Errorf("debug string leaks a directory path: %q", res.Debug)
	}
}

func TestRemoveBookmark(t *testing.T) {
	src := threePager(t)
	dir := t.TempDir()
	added := filepath.Join(dir, "added.pdf")
	removed := filepath.Join(dir, "removed.pdf")
	svc := &Service{}

	if res := svc.AddBookmark(src, added, "Doomed", 2); res.Error != "" {
		t.Fatalf("AddBookmark: %s", res.Error)
	}
	if res := svc.RemoveBookmark(added, removed, "Doomed", 2); res.Error != "" {
		t.Fatalf("RemoveBookmark: %s", res.Error)
	}

	got, err := svc.ListBookmarks(removed)
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d bookmarks, want 0: %+v", len(got), got)
	}
}

func TestRemoveBookmark_NotFound(t *testing.T) {
	src := threePager(t)
	dir := t.TempDir()
	added := filepath.Join(dir, "added.pdf")
	svc := &Service{}

	if res := svc.AddBookmark(src, added, "Real", 1); res.Error != "" {
		t.Fatalf("AddBookmark: %s", res.Error)
	}
	if res := svc.RemoveBookmark(added, filepath.Join(dir, "out.pdf"), "Imaginary", 1); res.Error == "" {
		t.Error("removing a bookmark that does not exist reported success")
	}
}

// RemoveBookmark leaves a .stripped.pdf next to its output while it works; it
// must clean that up.
func TestRemoveBookmark_CleansUpIntermediate(t *testing.T) {
	src := threePager(t)
	dir := t.TempDir()
	added := filepath.Join(dir, "added.pdf")
	out := filepath.Join(dir, "out.pdf")
	svc := &Service{}

	if res := svc.AddBookmark(src, added, "Doomed", 1); res.Error != "" {
		t.Fatalf("AddBookmark: %s", res.Error)
	}
	if res := svc.RemoveBookmark(added, out, "Doomed", 1); res.Error != "" {
		t.Fatalf("RemoveBookmark: %s", res.Error)
	}

	entries, err := filepath.Glob(filepath.Join(dir, "*.stripped.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("intermediate files left behind: %v", entries)
	}
}
