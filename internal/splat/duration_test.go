package splat

import (
	"testing"
	"time"
)

func TestDurationRoundTrip(t *testing.T) {
	cases := []struct {
		input    string
		wantSecs int64
		wantStr  string
	}{
		{"04:00:00", 14400, "04:00:00"},
		{"1:30:00", 5400, "01:30:00"},
		{"00:30:00", 1800, "00:30:00"},
		{"PT4H", 14400, "04:00:00"},
		{"PT4H30M", 16200, "04:30:00"},
		{"P1DT2H3M4S", 93784, "26:03:04"},
		{"3600", 3600, "01:00:00"},
		{"14400", 14400, "04:00:00"},
		{"6:0:0", 21600, "06:00:00"},      // unpadded fields
		{"0:00:10:00", 600, "00:10:00"},   // four-field D:HH:MM:SS
		{"1:02:03:04", 93784, "26:03:04"}, // one day, colon form
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			var d Duration
			if err := d.parse(tc.input); err != nil {
				t.Fatalf("parse(%q): %v", tc.input, err)
			}
			if got := int64(d.Duration().Seconds()); got != tc.wantSecs {
				t.Errorf("seconds: got %d, want %d", got, tc.wantSecs)
			}
			if got := d.String(); got != tc.wantStr {
				t.Errorf("String(): got %q, want %q", got, tc.wantStr)
			}
		})
	}
}

func TestDurationJSON(t *testing.T) {
	d := DurationOf(4 * time.Hour)
	data, err := d.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"04:00:00"` {
		t.Errorf("MarshalJSON: got %s", data)
	}

	var d2 Duration
	if err := d2.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if d2.Duration() != 4*time.Hour {
		t.Errorf("round-trip mismatch: got %v", d2.Duration())
	}
}

func TestDurationInvalidInput(t *testing.T) {
	cases := []string{"", "not-a-duration", "P", "1h30m"}
	for _, tc := range cases {
		var d Duration
		if err := d.parse(tc); err == nil {
			t.Errorf("parse(%q): expected error, got nil", tc)
		}
	}
}
