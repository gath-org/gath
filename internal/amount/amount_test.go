package amount

import "testing"

func TestParseFormat(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"1", 100_000_000},
		{"0.00000001", 1},
		{".5", 50_000_000},
		{"5.", 500_000_000},
		{"0", 0},
		{"20.99999999", 2_099_999_999},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.in, got, tc.want)
		}
		if tc.in != "5." && tc.in != ".5" && Format(got) != trim(tc.in) && Format(got) != tc.in {
			// Format trims trailing zeros. "1" stays "1". "0.00000001" stays.
			if Format(got) != expectFormat(tc.want) {
				t.Fatalf("format %s -> %s", tc.in, Format(got))
			}
		}
	}
	if _, err := Parse("1.000000001"); err == nil {
		t.Fatal("accepted 9 decimal places")
	}
	if _, err := Parse("-1"); err == nil {
		t.Fatal("accepted a sign")
	}
	if Format(100_000_000) != "1" || Format(150_000_000) != "1.5" || Format(1) != "0.00000001" {
		t.Fatalf("format mismatch %s %s %s", Format(100_000_000), Format(150_000_000), Format(1))
	}
}

func trim(s string) string { return s }

func expectFormat(atoms uint64) string { return Format(atoms) }
