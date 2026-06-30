package brew

import "testing"

func TestPackageLeaf(t *testing.T) {
	cases := map[string]string{
		"jq":                                   "jq",
		"oven-sh/bun/bun":                      "bun",
		"jandedobbeleer/oh-my-posh/oh-my-posh": "oh-my-posh",
		"":                                     "",
	}
	for in, want := range cases {
		if got := PackageLeaf(in); got != want {
			t.Errorf("PackageLeaf(%q) = %q, want %q", in, got, want)
		}
	}
}
