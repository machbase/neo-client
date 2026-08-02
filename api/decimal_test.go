package api

import (
	"math/big"
	"testing"
)

func TestParseDecimal(t *testing.T) {
	tests := []struct {
		input     string
		precision int
		scale     int
		want      string
	}{
		{"0", 10, 2, "0.00"},
		{"123.454", 10, 2, "123.45"},
		{"123.455", 10, 2, "123.46"},
		{"-0.005", 10, 2, "-0.01"},
		{".5", 3, 2, "0.50"},
		{"99999999999999999999999999999999999999999999999999999999999999999", 65, 0, "99999999999999999999999999999999999999999999999999999999999999999"},
	}
	for _, tc := range tests {
		d, err := ParseDecimal(tc.input, tc.precision, tc.scale)
		if err != nil {
			t.Fatalf("ParseDecimal(%q) error = %v", tc.input, err)
		}
		if got := d.String(); got != tc.want {
			t.Fatalf("ParseDecimal(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDecimalValidationAndCopy(t *testing.T) {
	for _, tc := range []struct {
		value            string
		precision, scale int
	}{
		{"1", 0, 0}, {"1", 66, 0}, {"1", 10, 31}, {"1", 2, 3}, {"100", 2, 0}, {"x", 10, 2},
	} {
		if _, err := ParseDecimal(tc.value, tc.precision, tc.scale); err == nil {
			t.Fatalf("ParseDecimal(%q, %d, %d) expected error", tc.value, tc.precision, tc.scale)
		}
	}
	unscaled := big.NewInt(123)
	d, err := NewDecimal(unscaled, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	unscaled.SetInt64(999)
	copyValue := d.Unscaled()
	copyValue.SetInt64(777)
	if d.String() != "1.23" {
		t.Fatalf("decimal was mutated: %s", d.String())
	}
}
