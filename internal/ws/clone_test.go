package ws

import "testing"

func TestCloneBasename(t *testing.T) {
	for _, tc := range []struct{ remote, want string }{
		{"git@github.com:d1ys3nk0/m111.git", "m111"},
		{"host:repository.git", "repository"},
		{"ssh://user@host:2222/group/repo.git", "repo"},
		{"https://user:password@host/group/repo.git/", "repo"},
		{"../workspace/.git", "workspace"},
		{"/tmp/my  workspace.git", "my workspace"},
		{"ssh://host:2222/", "host"},
		{"/", ""},
	} {
		if got := cloneBasename(tc.remote, false); got != tc.want {
			t.Errorf("cloneBasename(%q) = %q, want %q", tc.remote, got, tc.want)
		}
	}
}
