package fileurl

import "testing"

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
