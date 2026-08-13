package api

import (
	"database/sql/driver"
	"fmt"
	"math/big"
	"strings"
)

const (
	DecimalMaxPrecision = 65
	DecimalMaxScale     = 30
)

// Decimal is an exact fixed-point value. The represented value is
// Unscaled() * 10^-Scale().
type Decimal struct {
	unscaled  big.Int
	precision int
	scale     int
}

// NewDecimal constructs an exact decimal from an unscaled integer.
func NewDecimal(unscaled *big.Int, precision, scale int) (Decimal, error) {
	if unscaled == nil {
		return Decimal{}, fmt.Errorf("decimal unscaled value is nil")
	}
	if err := validateDecimalPrecisionScale(precision, scale); err != nil {
		return Decimal{}, err
	}
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(precision)), nil)
	if new(big.Int).Abs(new(big.Int).Set(unscaled)).Cmp(limit) >= 0 {
		return Decimal{}, fmt.Errorf("decimal value exceeds precision %d", precision)
	}
	var ret Decimal
	ret.unscaled.Set(unscaled)
	ret.precision = precision
	ret.scale = scale
	return ret, nil
}

// ParseDecimal parses text and rounds excess fractional digits half away from
// zero to the requested scale.
func ParseDecimal(text string, precision, scale int) (Decimal, error) {
	if err := validateDecimalPrecisionScale(precision, scale); err != nil {
		return Decimal{}, err
	}
	s := strings.TrimSpace(text)
	if s == "" {
		return Decimal{}, fmt.Errorf("invalid decimal value %q", text)
	}
	negative := false
	if s[0] == '+' || s[0] == '-' {
		negative = s[0] == '-'
		s = s[1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 || (len(parts) == 1 && parts[0] == "") ||
		(len(parts) == 2 && parts[0] == "" && parts[1] == "") {
		return Decimal{}, fmt.Errorf("invalid decimal value %q", text)
	}
	integer := parts[0]
	if integer == "" {
		integer = "0"
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	for _, digits := range []string{integer, fraction} {
		for _, ch := range digits {
			if ch < '0' || ch > '9' {
				return Decimal{}, fmt.Errorf("invalid decimal value %q", text)
			}
		}
	}
	roundUp := len(fraction) > scale && fraction[scale] >= '5'
	if len(fraction) > scale {
		fraction = fraction[:scale]
	}
	if len(fraction) < scale {
		fraction += strings.Repeat("0", scale-len(fraction))
	}
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		digits = "0"
	}
	unscaled := new(big.Int)
	if _, ok := unscaled.SetString(digits, 10); !ok {
		return Decimal{}, fmt.Errorf("invalid decimal value %q", text)
	}
	if roundUp {
		unscaled.Add(unscaled, big.NewInt(1))
	}
	if negative && unscaled.Sign() != 0 {
		unscaled.Neg(unscaled)
	}
	return NewDecimal(unscaled, precision, scale)
}

func validateDecimalPrecisionScale(precision, scale int) error {
	if precision < 1 || precision > DecimalMaxPrecision {
		return fmt.Errorf("invalid decimal precision %d", precision)
	}
	if scale < 0 || scale > DecimalMaxScale || scale > precision {
		return fmt.Errorf("invalid decimal scale %d for precision %d", scale, precision)
	}
	return nil
}

func (d Decimal) Precision() int { return d.precision }

func (d Decimal) Scale() int { return d.scale }

// Unscaled returns a copy of the underlying integer.
func (d Decimal) Unscaled() *big.Int { return new(big.Int).Set(&d.unscaled) }

func (d Decimal) String() string {
	digits := new(big.Int).Abs(new(big.Int).Set(&d.unscaled)).String()
	if d.scale > 0 {
		if len(digits) <= d.scale {
			digits = strings.Repeat("0", d.scale-len(digits)+1) + digits
		}
		cut := len(digits) - d.scale
		digits = digits[:cut] + "." + digits[cut:]
	}
	if d.unscaled.Sign() < 0 {
		return "-" + digits
	}
	return digits
}

// Value implements driver.Valuer. database/sql receives DECIMAL as exact text.
func (d Decimal) Value() (driver.Value, error) { return d.String(), nil }

// Scan implements sql.Scanner. Text input keeps the receiver's precision and
// scale; an uninitialized receiver uses DECIMAL(65,30).
func (d *Decimal) Scan(src any) error {
	if d == nil {
		return fmt.Errorf("cannot scan decimal into nil receiver")
	}
	precision, scale := d.precision, d.scale
	if precision == 0 {
		precision, scale = DecimalMaxPrecision, DecimalMaxScale
	}
	var text string
	switch v := src.(type) {
	case Decimal:
		*d = v
		return nil
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return fmt.Errorf("cannot scan %T as decimal", src)
	}
	parsed, err := ParseDecimal(text, precision, scale)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
