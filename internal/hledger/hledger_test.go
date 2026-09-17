package hledger

import (
	"reflect"
	"testing"
)

func TestParseBalanceCSV(t *testing.T) {
	cases := []struct {
		name string
		csv  string
		want []Balance
	}{
		{
			name: "typical output with currency symbols",
			csv: "\"account\",\"balance\"\n" +
				"\"assets:bank:checking\",\"$1,234.56\"\n" +
				"\"expenses:food\",\"$45.00\"\n" +
				"\"income:salary\",\"$-2,000.00\"\n",
			want: []Balance{
				{Account: "assets:bank:checking", Amount: 1234.56},
				{Account: "expenses:food", Amount: 45.00},
				{Account: "income:salary", Amount: -2000.00},
			},
		},
		{
			name: "skips trailing total row",
			csv: "\"account\",\"balance\"\n" +
				"\"assets:cash\",\"$10.00\"\n" +
				"\"total\",\"$10.00\"\n",
			want: []Balance{
				{Account: "assets:cash", Amount: 10.00},
			},
		},
		{
			name: "empty report",
			csv:  "\"account\",\"balance\"\n",
			want: []Balance{},
		},
		{
			name: "negative without currency symbol",
			csv: "\"account\",\"balance\"\n" +
				"\"liabilities:creditcard\",\"-123.45\"\n",
			want: []Balance{
				{Account: "liabilities:creditcard", Amount: -123.45},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBalanceCSV([]byte(tc.csv))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseAmount(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"$1,234.56", 1234.56},
		{"$-2,000.00", -2000.00},
		{"-123.45", -123.45},
		{"0", 0},
		{"$0.00", 0},
	}
	for _, tc := range cases {
		got, err := parseAmount(tc.in)
		if err != nil {
			t.Fatalf("parseAmount(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseAmount(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
