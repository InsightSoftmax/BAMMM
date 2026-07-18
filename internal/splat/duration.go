package splat

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MarshalJSON emits the duration as a quoted HH:MM:SS string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON accepts ISO 8601 (PT4H30M), HH:MM:SS, or bare integer seconds.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return d.parse(s)
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("duration: expected string or integer, got %s", data)
	}
	d.d = time.Duration(n) * time.Second
	return nil
}

// String returns the duration as HH:MM:SS (Slurm/PBS canonical form).
func (d Duration) String() string {
	total := int64(d.d.Seconds())
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// iso8601Re matches P[n]DT[n]H[n]M[n[.n]S — no year/month (ambiguous length).
var iso8601Re = regexp.MustCompile(
	`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`,
)

// Slurm time formats (most specific first):
//
//	D-HH:MM:SS   days-hours:minutes:seconds
//	D-HH:MM      days-hours:minutes
//	HH:MM:SS
//	HH:MM        (no seconds)
//	MM           bare minutes
//
// Minute/second fields need not be zero-padded (real-world walltimes like
// "6:0:0" occur). A four-field colon form D:HH:MM:SS is also accepted.
var (
	dHHMMSSRe   = regexp.MustCompile(`^(\d+)-(\d+):(\d{1,2}):(\d{1,2})$`)
	dHHMMRe     = regexp.MustCompile(`^(\d+)-(\d+):(\d{1,2})$`)
	dColonHMSRe = regexp.MustCompile(`^(\d+):(\d{1,2}):(\d{1,2}):(\d{1,2})$`)
	hhmmssRe    = regexp.MustCompile(`^(\d+):(\d{1,2}):(\d{1,2})$`)
	hhmmRe      = regexp.MustCompile(`^(\d+):(\d{1,2})$`)
)

func (d *Duration) parse(s string) error {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "P") {
		return d.parseISO8601(s)
	}
	// D-HH:MM:SS
	if m := dHHMMSSRe.FindStringSubmatch(s); m != nil {
		days, _ := strconv.ParseInt(m[1], 10, 64)
		h, _ := strconv.ParseInt(m[2], 10, 64)
		min, _ := strconv.ParseInt(m[3], 10, 64)
		sec, _ := strconv.ParseInt(m[4], 10, 64)
		d.d = time.Duration(days*86400+h*3600+min*60+sec) * time.Second
		return nil
	}
	// D-HH:MM
	if m := dHHMMRe.FindStringSubmatch(s); m != nil {
		days, _ := strconv.ParseInt(m[1], 10, 64)
		h, _ := strconv.ParseInt(m[2], 10, 64)
		min, _ := strconv.ParseInt(m[3], 10, 64)
		d.d = time.Duration(days*86400+h*3600+min*60) * time.Second
		return nil
	}
	// D:HH:MM:SS (four colon-separated fields)
	if m := dColonHMSRe.FindStringSubmatch(s); m != nil {
		days, _ := strconv.ParseInt(m[1], 10, 64)
		h, _ := strconv.ParseInt(m[2], 10, 64)
		min, _ := strconv.ParseInt(m[3], 10, 64)
		sec, _ := strconv.ParseInt(m[4], 10, 64)
		d.d = time.Duration(days*86400+h*3600+min*60+sec) * time.Second
		return nil
	}
	// HH:MM:SS
	if m := hhmmssRe.FindStringSubmatch(s); m != nil {
		h, _ := strconv.ParseInt(m[1], 10, 64)
		min, _ := strconv.ParseInt(m[2], 10, 64)
		sec, _ := strconv.ParseInt(m[3], 10, 64)
		d.d = time.Duration(h*3600+min*60+sec) * time.Second
		return nil
	}
	// HH:MM
	if m := hhmmRe.FindStringSubmatch(s); m != nil {
		h, _ := strconv.ParseInt(m[1], 10, 64)
		min, _ := strconv.ParseInt(m[2], 10, 64)
		d.d = time.Duration(h*60+min) * time.Minute
		return nil
	}
	// Bare integer: seconds
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("duration: cannot parse %q (expected ISO 8601, [D-]HH:MM[:SS], or integer seconds)", s)
	}
	d.d = time.Duration(n) * time.Second
	return nil
}

func (d *Duration) parseISO8601(s string) error {
	m := iso8601Re.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "" && m[3] == "" && m[4] == "") {
		return fmt.Errorf("duration: invalid ISO 8601 %q (no components found)", s)
	}
	var total float64
	if m[1] != "" {
		v, _ := strconv.ParseFloat(m[1], 64)
		total += v * 86400
	}
	if m[2] != "" {
		v, _ := strconv.ParseFloat(m[2], 64)
		total += v * 3600
	}
	if m[3] != "" {
		v, _ := strconv.ParseFloat(m[3], 64)
		total += v * 60
	}
	if m[4] != "" {
		v, _ := strconv.ParseFloat(m[4], 64)
		total += v
	}
	d.d = time.Duration(total * float64(time.Second))
	return nil
}
