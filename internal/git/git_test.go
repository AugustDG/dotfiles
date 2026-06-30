package git

import "testing"

func TestSSHToHTTPS(t *testing.T) {
	cases := map[string]string{
		"git@github.com:AugustDG/dotfiles.git": "https://github.com/AugustDG/dotfiles.git",
		"git@gitlab.com:group/sub/repo.git":    "https://gitlab.com/group/sub/repo.git",
	}
	for in, want := range cases {
		if got := sshToHTTPS(in); got != want {
			t.Errorf("sshToHTTPS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAtoi(t *testing.T) {
	cases := map[string]int{"0": 0, "5": 5, "42": 42, "": 0, "12x": 12, "x": 0}
	for in, want := range cases {
		if got := atoi(in); got != want {
			t.Errorf("atoi(%q) = %d, want %d", in, got, want)
		}
	}
}
