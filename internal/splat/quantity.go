package splat

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	_KiB int64 = 1024
	_MiB int64 = 1024 * _KiB
	_GiB int64 = 1024 * _MiB
	_TiB int64 = 1024 * _GiB
	_KB  int64 = 1000
	_MB  int64 = 1000 * _KB
	_GB  int64 = 1000 * _MB
	_TB  int64 = 1000 * _GB
)

// QuantityOf creates a Quantity from a raw byte count.
func QuantityOf(bytes int64) *Quantity { return &Quantity{bytes: bytes} }

// Bytes returns the canonical byte count.
func (q Quantity) Bytes() int64 { return q.bytes }

// String returns the most compact IEC representation (Gi > Mi > Ki > bytes).
func (q Quantity) String() string {
	switch {
	case q.bytes%_TiB == 0:
		return fmt.Sprintf("%dTi", q.bytes/_TiB)
	case q.bytes%_GiB == 0:
		return fmt.Sprintf("%dGi", q.bytes/_GiB)
	case q.bytes%_MiB == 0:
		return fmt.Sprintf("%dMi", q.bytes/_MiB)
	case q.bytes%_KiB == 0:
		return fmt.Sprintf("%dKi", q.bytes/_KiB)
	default:
		return strconv.FormatInt(q.bytes, 10)
	}
}

func (q Quantity) MarshalJSON() ([]byte, error) {
	return json.Marshal(q.String())
}

func (q *Quantity) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return q.parse(s)
	}
	// Bare integer in JSON: treat as MiB (common HPC convention).
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("quantity: expected string or integer, got %s", data)
	}
	q.bytes = n * _MiB
	return nil
}

// parse accepts: 4Ti/4Gi/4Mi/4Ki (IEC), 4T/4G/4M/4K (SI decimal), bare int (MiB).
func (q *Quantity) parse(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("quantity: empty string")
	}
	// IEC binary suffixes — check two-char before one-char.
	switch {
	case strings.HasSuffix(s, "Ti"):
		return q.parseWithMult(s, "Ti", _TiB, false)
	case strings.HasSuffix(s, "Gi"):
		return q.parseWithMult(s, "Gi", _GiB, false)
	case strings.HasSuffix(s, "Mi"):
		return q.parseWithMult(s, "Mi", _MiB, false)
	case strings.HasSuffix(s, "Ki"):
		return q.parseWithMult(s, "Ki", _KiB, false)
	// SI decimal suffixes.
	case strings.HasSuffix(s, "T"):
		return q.parseWithMult(s, "T", _TB, true)
	case strings.HasSuffix(s, "G"):
		return q.parseWithMult(s, "G", _GB, true)
	case strings.HasSuffix(s, "M"):
		return q.parseWithMult(s, "M", _MB, true)
	case strings.HasSuffix(s, "K"):
		return q.parseWithMult(s, "K", _KB, true)
	}
	// Bare integer: treat as MiB.
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("quantity: cannot parse %q", s)
	}
	q.bytes = n * _MiB
	return nil
}

func (q *Quantity) parseWithMult(s, suffix string, mult int64, allowFloat bool) error {
	raw := strings.TrimSuffix(s, suffix)
	if allowFloat {
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || f < 0 {
			return fmt.Errorf("quantity: invalid value %q", s)
		}
		q.bytes = int64(f * float64(mult))
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return fmt.Errorf("quantity: invalid value %q", s)
	}
	q.bytes = n * mult
	return nil
}
