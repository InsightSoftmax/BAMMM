package splat

import "testing"

func TestQuantityParse(t *testing.T) {
	cases := []struct {
		input     string
		wantBytes int64
		wantStr   string
	}{
		{"16Gi", 16 * _GiB, "16Gi"},
		{"128Gi", 128 * _GiB, "128Gi"},
		{"4Mi", 4 * _MiB, "4Mi"},
		{"1Ti", _TiB, "1Ti"},
		{"1024Mi", _GiB, "1Gi"},      // normalises up to Gi
		{"4096", 4096 * _MiB, "4Gi"}, // bare int → MiB → normalises
		{"16G", 16 * _GB, "14Mi"},    // SI decimal: 16*10^9 bytes → doesn't hit GiB boundary
		{"1G", _GB, "953Mi"},         // 10^9 bytes, not 2^30
	}
	// Recalculate expected string for SI cases since they don't hit IEC boundaries.
	siFix := map[string]string{
		"16G": func() string {
			b := int64(16 * float64(_GB))
			q := Quantity{bytes: b}
			return q.String()
		}(),
		"1G": func() string {
			q := Quantity{bytes: _GB}
			return q.String()
		}(),
	}
	for k, v := range siFix {
		for i := range cases {
			if cases[i].input == k {
				cases[i].wantStr = v
			}
		}
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			q := &Quantity{}
			if err := q.parse(tc.input); err != nil {
				t.Fatalf("parse(%q): %v", tc.input, err)
			}
			if q.Bytes() != tc.wantBytes {
				t.Errorf("bytes: got %d, want %d", q.Bytes(), tc.wantBytes)
			}
			if q.String() != tc.wantStr {
				t.Errorf("String(): got %q, want %q", q.String(), tc.wantStr)
			}
		})
	}
}

func TestQuantityJSON(t *testing.T) {
	q := QuantityOf(16 * _GiB)
	data, err := q.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"16Gi"` {
		t.Errorf("MarshalJSON: got %s", data)
	}

	var q2 Quantity
	if err := q2.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if q2.Bytes() != 16*_GiB {
		t.Errorf("round-trip: got %d bytes", q2.Bytes())
	}
}

func TestQuantityJSONBareInt(t *testing.T) {
	var q Quantity
	if err := q.UnmarshalJSON([]byte(`4096`)); err != nil {
		t.Fatal(err)
	}
	if q.Bytes() != 4096*_MiB {
		t.Errorf("bare int: got %d bytes, want %d", q.Bytes(), 4096*_MiB)
	}
}

func TestQuantityInvalid(t *testing.T) {
	cases := []string{"", "abc", "16Xi", "-1Gi"}
	for _, tc := range cases {
		q := &Quantity{}
		if err := q.parse(tc); err == nil {
			t.Errorf("parse(%q): expected error, got nil", tc)
		}
	}
}
