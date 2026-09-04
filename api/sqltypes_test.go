package api

import "testing"

func TestSqlTypeDataTypeArrayVariants(t *testing.T) {
	tests := []struct {
		sqlType SqlType
		want    DataType
	}{
		{SqlTypeInt16Array, DataTypeInt16Array},
		{SqlTypeUInt16Array, DataTypeUInt16Array},
		{SqlTypeInt32Array, DataTypeInt32Array},
		{SqlTypeUInt32Array, DataTypeUInt32Array},
		{SqlTypeInt64Array, DataTypeInt64Array},
		{SqlTypeUInt64Array, DataTypeUInt64Array},
		{SqlTypeFloatArray, DataTypeFloatArray},
		{SqlTypeDoubleArray, DataTypeDoubleArray},
		{SqlTypeDecimalArray, DataTypeDecimalArray},
		{SqlTypeDecimal, DataTypeDecimal},
	}
	for _, tc := range tests {
		t.Run(tc.sqlType.String(), func(t *testing.T) {
			if got := tc.sqlType.DataType(); got != tc.want {
				t.Fatalf("DataType()=%v, want %v", got, tc.want)
			}
		})
	}
}
