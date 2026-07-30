package fileurl

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestFromPathUsesPortableFileURLForms(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "windows drive backslashes",
			path: `C:\x\y\node.hello.schema.json`,
			want: "file:///C:/x/y/node.hello.schema.json",
		},
		{
			name: "windows drive slashes",
			path: "D:/a/thirdshift/thirdshift/packages/protocol/schemas/node.hello.schema.json",
			want: "file:///D:/a/thirdshift/thirdshift/packages/protocol/schemas/node.hello.schema.json",
		},
		{
			name: "posix absolute",
			path: "/tmp/thirdshift/schemas/node.hello.schema.json",
			want: "file:///tmp/thirdshift/schemas/node.hello.schema.json",
		},
		{
			name: "unc path",
			path: `\\server\share\schemas\node.hello.schema.json`,
			want: "file://server/share/schemas/node.hello.schema.json",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := FromPath(test.path)
			if err != nil {
				t.Fatalf("from path: %v", err)
			}
			if got != test.want {
				t.Fatalf("FromPath(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestToPathForHandlesWindowsDriveURLs(t *testing.T) {
	cases := []struct {
		goos, raw, want string
	}{
		{"windows", "file:///D:/a/x/schema.json", "D:/a/x/schema.json"},
		{"windows", "file:///c:/Users/x/y.gguf", "c:/Users/x/y.gguf"},
		{"linux", "file:///home/x/y.json", "/home/x/y.json"},
		{"darwin", "file:///tmp/x.json", "/tmp/x.json"},
	}
	for _, tc := range cases {
		parsed, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		got := filepath.ToSlash(toPathFor(tc.goos, parsed))
		if got != tc.want {
			t.Errorf("toPathFor(%s, %s) = %q, want %q", tc.goos, tc.raw, got, tc.want)
		}
	}
}

func TestIsLocalRawURLTreatsDrivePathsAsLocal(t *testing.T) {
	cases := map[string]bool{
		`c:\Users\x\new.gguf`: true,
		`/tmp/x/new.gguf`:     true,
		`relative/new.gguf`:   true,
		`https://x.example/y`: false,
		`file:///D:/a/x.json`: false,
	}
	for raw, want := range cases {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got := IsLocalRawURL(parsed); got != want {
			t.Errorf("IsLocalRawURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
