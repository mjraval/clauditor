package usage

import "testing"

func TestFormatUSD(t *testing.T) {
	cases := []struct {
		micro int64
		want  string
	}{
		{0, "$0.00"},
		{3_400_000, "$3.40"},
		{50313, "$0.05"},
		{1_000_000_000, "$1000.00"},
	}
	for _, c := range cases {
		if got := FormatUSD(c.micro); got != c.want {
			t.Errorf("FormatUSD(%d) = %q, want %q", c.micro, got, c.want)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{820, "820"},
		{1200, "1.2k"},
		{450_000, "450.0k"},
		{1_200_000, "1.2M"},
	}
	for _, c := range cases {
		if got := FormatTokens(c.n); got != c.want {
			t.Errorf("FormatTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
