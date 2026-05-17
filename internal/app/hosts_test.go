package app

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// parseHosts
// ---------------------------------------------------------------------------

func TestParseHosts_Plain(t *testing.T) {
	t.Parallel()
	got := parseHosts("my-app.beacon")
	want := []string{"my-app.beacon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHosts_MultipleComma(t *testing.T) {
	t.Parallel()
	got := parseHosts("app.beacon, www.beacon")
	want := []string{"app.beacon", "www.beacon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHosts_BraceAlternation(t *testing.T) {
	t.Parallel()
	got := parseHosts("{foo,bar,baz}.lh")
	want := []string{"foo.lh", "bar.lh", "baz.lh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHosts_MixedPlainAndBrace(t *testing.T) {
	t.Parallel()
	got := parseHosts("my-app.beacon,{foo,bar,baz}.lh")
	want := []string{"my-app.beacon", "foo.lh", "bar.lh", "baz.lh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHosts_NumericRange(t *testing.T) {
	t.Parallel()
	got := parseHosts("node{1..4}.lh")
	want := []string{"node1.lh", "node2.lh", "node3.lh", "node4.lh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHosts_LargeRange(t *testing.T) {
	t.Parallel()
	got := parseHosts("node{1..16}.lh")
	if len(got) != 16 {
		t.Fatalf("expected 16 hosts, got %d: %v", len(got), got)
	}
	if got[0] != "node1.lh" || got[15] != "node16.lh" {
		t.Errorf("unexpected range endpoints: %s .. %s", got[0], got[15])
	}
}

func TestParseHosts_MixedBraceAndRange(t *testing.T) {
	t.Parallel()
	got := parseHosts("my-app.beacon,{foo,bar,baz}.lh,node{1..3}.lh")
	want := []string{
		"my-app.beacon",
		"foo.lh", "bar.lh", "baz.lh",
		"node1.lh", "node2.lh", "node3.lh",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHosts_EmptyAndExtraCommas(t *testing.T) {
	t.Parallel()
	got := parseHosts("app.beacon,,,")
	want := []string{"app.beacon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHosts_EmptyString(t *testing.T) {
	t.Parallel()
	got := parseHosts("")
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// expandBraces
// ---------------------------------------------------------------------------

func TestExpandBraces_NoBraces(t *testing.T) {
	t.Parallel()
	got := expandBraces("app.beacon")
	want := []string{"app.beacon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandBraces_PrefixSuffix(t *testing.T) {
	t.Parallel()
	got := expandBraces("node-{a,b}.lh")
	want := []string{"node-a.lh", "node-b.lh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandBraces_NumericRange(t *testing.T) {
	t.Parallel()
	got := expandBraces("node{1..3}")
	want := []string{"node1", "node2", "node3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandBraces_NumericRangeDescending(t *testing.T) {
	t.Parallel()
	got := expandBraces("{3..1}")
	want := []string{"3", "2", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandBraces_NumericRangeSingleValue(t *testing.T) {
	t.Parallel()
	got := expandBraces("{5..5}")
	want := []string{"5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandBraces_UnmatchedBrace(t *testing.T) {
	t.Parallel()
	got := expandBraces("app.{beacon")
	want := []string{"app.{beacon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandBraces_NestedBraces(t *testing.T) {
	t.Parallel()
	// {a,{b,c}}.lh → a.lh, b.lh, c.lh
	got := expandBraces("{a,{b,c}}.lh")
	want := []string{"a.lh", "b.lh", "c.lh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// parseNumericRange
// ---------------------------------------------------------------------------

func TestParseNumericRange_Valid(t *testing.T) {
	t.Parallel()
	from, to, ok := parseNumericRange("1..16")
	if !ok || from != 1 || to != 16 {
		t.Errorf("got (%d, %d, %v), want (1, 16, true)", from, to, ok)
	}
}

func TestParseNumericRange_NoDots(t *testing.T) {
	t.Parallel()
	_, _, ok := parseNumericRange("foo")
	if ok {
		t.Error("expected false for non-range string")
	}
}

func TestParseNumericRange_NonNumeric(t *testing.T) {
	t.Parallel()
	_, _, ok := parseNumericRange("a..z")
	if ok {
		t.Error("expected false for non-numeric range")
	}
}
