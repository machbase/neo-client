package machnet

import (
	"fmt"
	"math/big"

	"github.com/machbase/neo-client/api"
)

var decimalSizes = [...]int{
	0,
	1, 2, 2, 2, 3, 3, 4, 4, 4, 5, 5, 6, 6,
	7, 7, 7, 8, 8, 9, 9, 9, 10, 10, 11, 11, 12,
	12, 12, 13, 13, 14, 14, 14, 15, 15, 16, 16, 17, 17,
	17, 18, 18, 19, 19, 19, 20, 20, 21, 21, 22, 22, 22,
	23, 23, 24, 24, 24, 25, 25, 26, 26, 26, 27, 27, 28,
}

func decimalSize(precision int) (int, error) {
	if precision < 1 || precision > api.DecimalMaxPrecision {
		return 0, fmt.Errorf("invalid decimal precision %d", precision)
	}
	return decimalSizes[precision], nil
}

func encodeDecimal(value any, precision, scale int) ([]byte, error) {
	size, err := decimalSize(precision)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return make([]byte, size), nil
	}
	var text string
	switch v := value.(type) {
	case api.Decimal:
		text = v.String()
	case *api.Decimal:
		if v == nil {
			return make([]byte, size), nil
		}
		text = v.String()
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		text = fmt.Sprint(v)
	}
	d, err := api.ParseDecimal(text, precision, scale)
	if err != nil {
		return nil, err
	}
	bits := uint(size * 8)
	valid := new(big.Int).Lsh(big.NewInt(1), bits-1)
	bias := new(big.Int).Lsh(big.NewInt(1), bits-2)
	encoded := new(big.Int).Add(valid, bias)
	encoded.Add(encoded, d.Unscaled())
	data := encoded.Bytes()
	if len(data) > size {
		return nil, fmt.Errorf("decimal value exceeds encoded width")
	}
	ret := make([]byte, size)
	copy(ret[size-len(data):], data)
	return ret, nil
}

func decodeDecimal(data []byte, precision, scale int) (any, error) {
	size, err := decimalSize(precision)
	if err != nil {
		return nil, err
	}
	if len(data) != size {
		return nil, fmt.Errorf("invalid decimal payload size %d, expected %d", len(data), size)
	}
	if data[0]&0x80 == 0 {
		for _, b := range data {
			if b != 0 {
				return nil, fmt.Errorf("invalid decimal payload")
			}
		}
		return nil, nil
	}
	bits := uint(size * 8)
	encoded := new(big.Int).SetBytes(data)
	valid := new(big.Int).Lsh(big.NewInt(1), bits-1)
	bias := new(big.Int).Lsh(big.NewInt(1), bits-2)
	unscaled := new(big.Int).Sub(encoded, valid)
	unscaled.Sub(unscaled, bias)
	return api.NewDecimal(unscaled, precision, scale)
}
