package looper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSettings(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sandbox.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestBypassArgs(t *testing.T) {
	assert.Equal(t, []string{"--dangerously-skip-permissions"}, BypassArgs())
}

func TestSandboxArgs_ForcesSandboxKeys(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty object",
			in:   `{}`,
			want: `{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true}}`,
		},
		{
			name: "forced keys set the other way",
			in:   `{"sandbox":{"enabled":false,"allowUnsandboxedCommands":true,"failIfUnavailable":false}}`,
			want: `{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true}}`,
		},
		{
			name: "everything else passes through",
			in: `{
  "permissions": {"deny": ["WebFetch"]},
  "sandbox": {
    "filesystem": {"denyRead": ["~/"], "allowWrite": ["/tmp/build"]},
    "network": {"allowedDomains": ["proxy.golang.org"], "httpProxyPort": 12345678901234567890}
  }
}`,
			want: `{"permissions":{"deny":["WebFetch"]},"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true,"filesystem":{"denyRead":["~/"],"allowWrite":["/tmp/build"]},"network":{"allowedDomains":["proxy.golang.org"],"httpProxyPort":12345678901234567890}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSettings(t, tt.in)

			args, err := SandboxArgs(path)
			require.NoError(t, err)
			assert.Equal(t, []string{"--permission-mode", "acceptEdits", "--settings", tt.want}, args)

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.in, string(after), "the settings file must not be written")
		})
	}
}

func TestSandboxArgs_RejectsBadFiles(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "empty file", in: ``, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "not JSON", in: `sandbox: true`, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "null", in: `null`, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "array", in: `[]`, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "string", in: `"x"`, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "sandbox null", in: `{"sandbox":null}`, wantErr: "--sandbox-settings: sandbox: must be a JSON object"},
		{name: "sandbox array", in: `{"sandbox":[]}`, wantErr: "--sandbox-settings: sandbox: must be a JSON object"},
		{name: "sandbox bool", in: `{"sandbox":true}`, wantErr: "--sandbox-settings: sandbox: must be a JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := SandboxArgs(writeSettings(t, tt.in))
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, args)
		})
	}
}

func TestSandboxArgs_UnreadablePath(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{filepath.Join(dir, "missing.json"), dir} {
		args, err := SandboxArgs(path)
		require.ErrorContains(t, err, "--sandbox-settings: file \""+path+"\" is not accessible")
		assert.Nil(t, args)
	}
}

func TestFolderSandbox(t *testing.T) {
	withFile := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withFile, "sandbox.json"), []byte("{}"), 0o600))
	empty := t.TempDir()
	withDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(withDir, "sandbox.json"), 0o700))

	cases := map[string]struct {
		dir  string
		want string
		ok   bool
	}{
		"regular file":               {dir: withFile, want: filepath.Join(withFile, "sandbox.json"), ok: true},
		"missing":                    {dir: empty},
		"a directory is ignored":     {dir: withDir},
		"folder that does not exist": {dir: filepath.Join(empty, "nope")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := FolderSandbox(tc.dir)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
