package projectupdate

import "testing"

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		current string
		minimum string
		want    bool
	}{
		{"0.1.0", "0.1.0", true},
		{"v0.2.0", "0.1.9", true},
		{"1.0.0", "0.9.9", true},
		{"0.1.0", "0.2.0", false},
	}
	for _, test := range tests {
		got, err := versionAtLeast(test.current, test.minimum)
		if err != nil || got != test.want {
			t.Fatalf("versionAtLeast(%q, %q) = %v, %v; want %v", test.current, test.minimum, got, err, test.want)
		}
	}
}
