package git

import (
	"reflect"
	"testing"
)

func TestParseHunks_SingleLineChange(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5,1 +5,1 @@ func old()
-old line
+new line`
	got := ParseHunks(diff)
	want := []Hunk{{Start: 5, End: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseHunks single-line change = %v, want %v", got, want)
	}
}

func TestParseHunks_MultiLineAddition(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -10,0 +11,5 @@ func existing()
+line1
+line2
+line3
+line4
+line5`
	got := ParseHunks(diff)
	want := []Hunk{{Start: 11, End: 15}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseHunks multi-line addition = %v, want %v", got, want)
	}
}

func TestParseHunks_PureDeletion(t *testing.T) {
	// count=0 means lines were only removed; hunk should be skipped.
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5,3 +4,0 @@ func removed()
-deleted1
-deleted2
-deleted3`
	got := ParseHunks(diff)
	if len(got) != 0 {
		t.Errorf("ParseHunks pure deletion = %v, want empty", got)
	}
}

func TestParseHunks_NewFile(t *testing.T) {
	// New file: single large hunk covering all lines.
	diff := `diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,42 @@
+package main
+// ... 42 lines`
	got := ParseHunks(diff)
	want := []Hunk{{Start: 1, End: 42}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseHunks new file = %v, want %v", got, want)
	}
}

func TestParseHunks_NoCount(t *testing.T) {
	// When there is no comma, count defaults to 1.
	diff := `@@ -1 +1 @@ package foo`
	got := ParseHunks(diff)
	want := []Hunk{{Start: 1, End: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseHunks no count = %v, want %v", got, want)
	}
}

func TestParseHunks_MultipleHunks(t *testing.T) {
	diff := `@@ -1,2 +1,3 @@ header
+added
@@ -10,1 +11,1 @@ middle
-old
+new
@@ -20,0 +21,2 @@ end
+line1
+line2`
	got := ParseHunks(diff)
	want := []Hunk{
		{Start: 1, End: 3},
		{Start: 11, End: 11},
		{Start: 21, End: 22},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseHunks multiple hunks = %v, want %v", got, want)
	}
}

func TestMergeHunks_Overlapping(t *testing.T) {
	hunks := []Hunk{
		{Start: 1, End: 5},
		{Start: 3, End: 8},
		{Start: 15, End: 20},
	}
	got := MergeHunks(hunks)
	want := []Hunk{
		{Start: 1, End: 8},
		{Start: 15, End: 20},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeHunks overlapping = %v, want %v", got, want)
	}
}

func TestMergeHunks_Adjacent(t *testing.T) {
	hunks := []Hunk{
		{Start: 1, End: 5},
		{Start: 6, End: 10},
	}
	got := MergeHunks(hunks)
	want := []Hunk{{Start: 1, End: 10}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeHunks adjacent = %v, want %v", got, want)
	}
}

func TestMergeHunks_Disjoint(t *testing.T) {
	hunks := []Hunk{
		{Start: 10, End: 15},
		{Start: 1, End: 5},
	}
	got := MergeHunks(hunks)
	want := []Hunk{
		{Start: 1, End: 5},
		{Start: 10, End: 15},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeHunks disjoint = %v, want %v", got, want)
	}
}

func TestMergeHunks_Empty(t *testing.T) {
	got := MergeHunks(nil)
	if got != nil {
		t.Errorf("MergeHunks nil = %v, want nil", got)
	}
}

func TestMergeHunks_Single(t *testing.T) {
	got := MergeHunks([]Hunk{{Start: 5, End: 10}})
	want := []Hunk{{Start: 5, End: 10}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeHunks single = %v, want %v", got, want)
	}
}

func TestInRange_InsideHunk(t *testing.T) {
	hunks := []Hunk{{Start: 5, End: 15}, {Start: 20, End: 25}}
	if !InRange(hunks, 8, 12) {
		t.Error("InRange: expected true for range fully inside a hunk")
	}
}

func TestInRange_OutsideAllHunks(t *testing.T) {
	hunks := []Hunk{{Start: 5, End: 15}, {Start: 20, End: 25}}
	if InRange(hunks, 16, 19) {
		t.Error("InRange: expected false for range between hunks")
	}
}

func TestInRange_PartialOverlap(t *testing.T) {
	hunks := []Hunk{{Start: 5, End: 15}, {Start: 20, End: 25}}
	if !InRange(hunks, 12, 18) {
		t.Error("InRange: expected true for range partially overlapping a hunk")
	}
}

func TestInRange_ExactBoundary(t *testing.T) {
	hunks := []Hunk{{Start: 5, End: 15}}
	if !InRange(hunks, 5, 5) {
		t.Error("InRange: expected true for range at hunk start boundary")
	}
	if !InRange(hunks, 15, 15) {
		t.Error("InRange: expected true for range at hunk end boundary")
	}
}

func TestInRange_EmptyHunks(t *testing.T) {
	if InRange(nil, 1, 10) {
		t.Error("InRange: expected false for nil hunks")
	}
}

func TestInRange_SpanningMultipleHunks(t *testing.T) {
	hunks := []Hunk{{Start: 5, End: 10}, {Start: 20, End: 25}}
	if !InRange(hunks, 1, 30) {
		t.Error("InRange: expected true for range spanning multiple hunks")
	}
}
