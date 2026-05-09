package restic

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"neubibackup/internal/config"
)

func TestBuildBackupArgs(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		contains []string
		excludes []string
	}{
		{
			name: "basic config",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home/user"},
				},
			},
			contains: []string{
				"backup",
				"--json",
				"-r", "/backup/repo",
				"--exclude-caches",
				"/home/user",
			},
		},
		{
			name: "with password file",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:         "/backup/repo",
					PasswordFile: "/path/to/password",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/data"},
				},
			},
			contains: []string{
				"--password-file", "/path/to/password",
			},
		},
		{
			name: "with password command",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:            "/backup/repo",
					PasswordCommand: "pass show backup",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/data"},
				},
			},
			contains: []string{
				"--password-command", "pass show backup",
			},
		},
		{
			name: "with excludes",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths:    []string{"/home"},
					Excludes: []string{"*.tmp", "node_modules", ".git"},
				},
			},
			contains: []string{
				"--exclude", "*.tmp",
				"--exclude", "node_modules",
				"--exclude", ".git",
			},
		},
		{
			name: "with exclude file",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths:       []string{"/home"},
					ExcludeFile: "/path/to/excludes.txt",
				},
			},
			contains: []string{
				"--exclude-file", "/path/to/excludes.txt",
			},
		},
		{
			name: "with global restic args",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/data"},
				},
				ResticArgs: config.ResticArgsConfig{
					Global: []string{"--verbose", "--quiet"},
				},
			},
			contains: []string{
				"--verbose", "--quiet",
			},
		},
		{
			name: "with backup-specific restic args",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/data"},
				},
				ResticArgs: config.ResticArgsConfig{
					Backup: []string{"--tag", "daily", "--compression", "max"},
				},
			},
			contains: []string{
				"--tag", "daily",
				"--compression", "max",
			},
		},
		{
			name: "user args override hardcoded flags",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/data"},
				},
				ResticArgs: config.ResticArgsConfig{
					Backup: []string{"--one-file-system"}, // User explicitly sets this
				},
			},
			contains: []string{
				"--one-file-system",
			},
		},
		{
			name: "multiple backup paths",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home/user/Documents", "/home/user/Pictures", "/etc"},
				},
			},
			contains: []string{
				"/home/user/Documents",
				"/home/user/Pictures",
				"/etc",
			},
		},
		{
			name: "with use_keychain",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:        "/backup/repo",
					UseKeychain: true,
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home"},
				},
			},
			contains: []string{
				"-r", "/backup/repo",
			},
			excludes: []string{
				"--password-file",
				"--password-command",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildBackupArgs(tt.cfg)

			// Check that all expected args are present
			for _, expected := range tt.contains {
				if !containsArg(args, expected) {
					t.Errorf("buildBackupArgs() missing expected arg %q, got: %v", expected, args)
				}
			}

			// Check that excluded args are not present
			for _, excluded := range tt.excludes {
				if containsArg(args, excluded) {
					t.Errorf("buildBackupArgs() should not contain %q, got: %v", excluded, args)
				}
			}

			// Verify "backup" is first and paths are last
			if len(args) < 2 {
				t.Fatalf("buildBackupArgs() returned too few args: %v", args)
			}
			if args[0] != "backup" {
				t.Errorf("buildBackupArgs() first arg should be 'backup', got %q", args[0])
			}

			// Paths should be at the end
			for _, path := range tt.cfg.Backup.Paths {
				found := false
				for i := len(args) - len(tt.cfg.Backup.Paths); i < len(args); i++ {
					if args[i] == path {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("buildBackupArgs() path %q should be at end of args, got: %v", path, args)
				}
			}
		})
	}
}

func TestBuildBackupArgs_WindowsFlag(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: config.BackupConfig{
			Paths: []string{"/data"},
		},
	}

	args := buildBackupArgs(cfg)

	if runtime.GOOS == "windows" {
		if !containsArg(args, "--use-fs-snapshot") {
			t.Error("buildBackupArgs() on Windows should include --use-fs-snapshot")
		}
	} else {
		if containsArg(args, "--use-fs-snapshot") {
			t.Error("buildBackupArgs() on non-Windows should not include --use-fs-snapshot")
		}
	}
}

func TestBuildBackupArgs_HardcodedFlags(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: config.BackupConfig{
			Paths: []string{"/data"},
		},
	}

	args := buildBackupArgs(cfg)

	// --exclude-caches should always be present on all platforms
	if !containsArg(args, "--exclude-caches") {
		t.Error("buildBackupArgs() should include --exclude-caches")
	}

	// --one-file-system should only be present on non-Windows
	if runtime.GOOS == "windows" {
		if containsArg(args, "--one-file-system") {
			t.Error("buildBackupArgs() on Windows should not include --one-file-system")
		}
	} else {
		if !containsArg(args, "--one-file-system") {
			t.Error("buildBackupArgs() on non-Windows should include --one-file-system")
		}
	}
}

func TestBuildBackupArgs_NoDuplicateFlags(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: config.BackupConfig{
			Paths: []string{"/data"},
		},
		ResticArgs: config.ResticArgsConfig{
			Backup: []string{"--one-file-system", "--exclude-caches"},
		},
	}

	args := buildBackupArgs(cfg)

	// Count occurrences of --exclude-caches (always hardcoded, should not be duplicated)
	excludeCachesCount := 0
	for _, arg := range args {
		if arg == "--exclude-caches" {
			excludeCachesCount++
		}
	}
	if excludeCachesCount > 1 {
		t.Errorf("buildBackupArgs() has duplicate --exclude-caches flags (%d occurrences)", excludeCachesCount)
	}

	// Count occurrences of --one-file-system (only hardcoded on non-Windows)
	oneFileSystemCount := 0
	for _, arg := range args {
		if arg == "--one-file-system" {
			oneFileSystemCount++
		}
	}
	// On non-Windows, user's --one-file-system should not be duplicated by hardcoded flag
	// On Windows, user's --one-file-system is kept as-is (no hardcoded addition)
	if oneFileSystemCount > 1 {
		t.Errorf("buildBackupArgs() has duplicate --one-file-system flags (%d occurrences)", oneFileSystemCount)
	}
}

func TestBuildEnv(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		proxyAddr string
		contains  map[string]string
	}{
		{
			name: "with direct password",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "mysecretpassword",
				},
			},
			proxyAddr: "",
			contains: map[string]string{
				"RESTIC_REPOSITORY": "/backup/repo",
				"RESTIC_PASSWORD":   "mysecretpassword",
			},
		},
		{
			name: "without direct password",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:         "/backup/repo",
					PasswordFile: "/path/to/password",
				},
			},
			proxyAddr: "",
			contains: map[string]string{
				"RESTIC_REPOSITORY": "/backup/repo",
			},
		},
		{
			name: "with proxy",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
			},
			proxyAddr: "127.0.0.1:1080",
			contains: map[string]string{
				"HTTP_PROXY":  "socks5://127.0.0.1:1080",
				"HTTPS_PROXY": "socks5://127.0.0.1:1080",
			},
		},
		{
			name: "without proxy",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
			},
			proxyAddr: "",
			contains:  map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := buildEnv(tt.cfg, tt.proxyAddr, fakePasswordSource{})
			if err != nil {
				t.Fatalf("buildEnv: %v", err)
			}

			// Convert to map for easier checking
			envMap := make(map[string]string)
			for _, e := range env {
				for i := 0; i < len(e); i++ {
					if e[i] == '=' {
						envMap[e[:i]] = e[i+1:]
						break
					}
				}
			}

			for key, expectedValue := range tt.contains {
				if value, ok := envMap[key]; !ok {
					t.Errorf("buildEnv() missing expected key %q", key)
				} else if value != expectedValue {
					t.Errorf("buildEnv() %s = %q, want %q", key, value, expectedValue)
				}
			}

			// Check that password is NOT set when using password file
			if tt.cfg.Repository.PasswordFile != "" {
				if _, ok := envMap["RESTIC_PASSWORD"]; ok {
					t.Error("buildEnv() should not set RESTIC_PASSWORD when using password file")
				}
			}

			// Check that proxy is NOT set when proxyAddr is empty
			if tt.proxyAddr == "" {
				if _, ok := envMap["HTTP_PROXY"]; ok {
					t.Error("buildEnv() should not set HTTP_PROXY when proxyAddr is empty")
				}
				if _, ok := envMap["HTTPS_PROXY"]; ok {
					t.Error("buildEnv() should not set HTTPS_PROXY when proxyAddr is empty")
				}
			}
		})
	}
}

func TestContainsArg(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		flag   string
		result bool
	}{
		{
			name:   "flag present",
			args:   []string{"--verbose", "--json", "--one-file-system"},
			flag:   "--json",
			result: true,
		},
		{
			name:   "flag not present",
			args:   []string{"--verbose", "--json"},
			flag:   "--quiet",
			result: false,
		},
		{
			name:   "empty args",
			args:   []string{},
			flag:   "--verbose",
			result: false,
		},
		{
			name:   "exact match required",
			args:   []string{"--verbose", "--json-output"},
			flag:   "--json",
			result: false,
		},
		{
			name:   "first element",
			args:   []string{"--first", "--second", "--third"},
			flag:   "--first",
			result: true,
		},
		{
			name:   "last element",
			args:   []string{"--first", "--second", "--third"},
			flag:   "--third",
			result: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsArg(tt.args, tt.flag)
			if result != tt.result {
				t.Errorf("containsArg(%v, %q) = %v, want %v", tt.args, tt.flag, result, tt.result)
			}
		})
	}
}

func TestBuildBackupArgs_ArgOrder(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:     "s3:bucket/path",
			Password: "secret",
		},
		Backup: config.BackupConfig{
			Paths:    []string{"/home", "/etc"},
			Excludes: []string{"*.tmp"},
		},
		ResticArgs: config.ResticArgsConfig{
			Global: []string{"--verbose"},
			Backup: []string{"--tag", "daily"},
		},
	}

	args := buildBackupArgs(cfg)

	// Find positions
	backupPos := -1
	repoFlagPos := -1
	firstPathPos := -1

	for i, arg := range args {
		switch {
		case arg == "backup" && backupPos == -1:
			backupPos = i
		case arg == "-r" && repoFlagPos == -1:
			repoFlagPos = i
		case arg == "/home" && firstPathPos == -1:
			firstPathPos = i
		}
	}

	// Verify order: backup command first
	if backupPos != 0 {
		t.Errorf("'backup' should be at position 0, got %d", backupPos)
	}

	// Verify repository flag comes before paths
	if repoFlagPos >= firstPathPos {
		t.Errorf("-r flag (pos %d) should come before paths (pos %d)", repoFlagPos, firstPathPos)
	}

	// Verify paths are at the end
	pathsStart := len(args) - len(cfg.Backup.Paths)
	for i, path := range cfg.Backup.Paths {
		if args[pathsStart+i] != path {
			t.Errorf("Path %q should be at position %d, got %q", path, pathsStart+i, args[pathsStart+i])
		}
	}
}

func TestSanitizeURLForLogging(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "REST backend with password",
			input:    "rest:https://user:secretpassword@backup.example.com/repo",
			expected: "rest:https://user:****@backup.example.com/repo",
		},
		{
			name:     "REST backend without password",
			input:    "rest:https://backup.example.com/repo",
			expected: "rest:https://backup.example.com/repo",
		},
		{
			name:     "SFTP with password",
			input:    "sftp://user:mypassword@server.example.com/backup",
			expected: "sftp://user:****@server.example.com/backup",
		},
		{
			name:     "local path",
			input:    "/backup/repo",
			expected: "/backup/repo",
		},
		{
			name:     "S3 bucket without credentials",
			input:    "s3:bucket-name/path/to/repo",
			expected: "s3:bucket-name/path/to/repo",
		},
		{
			name:     "HTTPS URL with password and port",
			input:    "rest:https://admin:supersecret123@backup.example.com:8080/restic",
			expected: "rest:https://admin:****@backup.example.com:8080/restic",
		},
		{
			name:     "URL with special characters in password",
			input:    "rest:https://user:p%40ss%2Fw0rd@backup.example.com/repo",
			expected: "rest:https://user:****@backup.example.com/repo",
		},
		{
			name:     "HTTP URL with password",
			input:    "rest:http://user:password@localhost:8000/backup",
			expected: "rest:http://user:****@localhost:8000/backup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeURLForLogging(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeURLForLogging(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeArgsForLogging(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "args with -r flag and password in URL",
			args:     []string{"backup", "-r", "rest:https://user:secret@server.com/repo", "/home"},
			expected: []string{"backup", "-r", "rest:https://user:****@server.com/repo", "/home"},
		},
		{
			name:     "args with --repo flag and password in URL",
			args:     []string{"backup", "--repo", "rest:https://user:secret@server.com/repo", "/home"},
			expected: []string{"backup", "--repo", "rest:https://user:****@server.com/repo", "/home"},
		},
		{
			name:     "args with --repo=value format",
			args:     []string{"backup", "--repo=rest:https://user:secret@server.com/repo", "/home"},
			expected: []string{"backup", "--repo=rest:https://user:****@server.com/repo", "/home"},
		},
		{
			name:     "args with -r=value format",
			args:     []string{"backup", "-r=rest:https://user:secret@server.com/repo", "/home"},
			expected: []string{"backup", "-r=rest:https://user:****@server.com/repo", "/home"},
		},
		{
			name:     "args without password in URL",
			args:     []string{"backup", "-r", "/local/repo", "/home"},
			expected: []string{"backup", "-r", "/local/repo", "/home"},
		},
		{
			name:     "original args not modified",
			args:     []string{"backup", "-r", "rest:https://user:secret@server.com/repo"},
			expected: []string{"backup", "-r", "rest:https://user:****@server.com/repo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to verify original isn't modified
			originalCopy := make([]string, len(tt.args))
			copy(originalCopy, tt.args)

			result := sanitizeArgsForLogging(tt.args)

			// Check result matches expected
			if len(result) != len(tt.expected) {
				t.Fatalf("sanitizeArgsForLogging() returned %d args, want %d", len(result), len(tt.expected))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("sanitizeArgsForLogging()[%d] = %q, want %q", i, result[i], tt.expected[i])
				}
			}

			// Verify original wasn't modified
			for i := range tt.args {
				if tt.args[i] != originalCopy[i] {
					t.Errorf("original args modified at [%d]: was %q, now %q", i, originalCopy[i], tt.args[i])
				}
			}
		})
	}
}

func TestBuildEnvUseKeychain(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:        "/backup/repo",
			UseKeychain: true,
		},
	}

	env, err := buildEnv(cfg, "", fakePasswordSource{pw: "from-keychain"})
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}

	var gotRepo, gotPw string
	var sawPw bool
	for _, e := range env {
		if strings.HasPrefix(e, "RESTIC_REPOSITORY=") {
			gotRepo = strings.TrimPrefix(e, "RESTIC_REPOSITORY=")
		}
		if strings.HasPrefix(e, "RESTIC_PASSWORD=") {
			gotPw = strings.TrimPrefix(e, "RESTIC_PASSWORD=")
			sawPw = true
		}
	}
	if gotRepo != "/backup/repo" {
		t.Errorf("RESTIC_REPOSITORY = %q, want /backup/repo", gotRepo)
	}
	if !sawPw {
		t.Error("RESTIC_PASSWORD missing")
	}
	if gotPw != "from-keychain" {
		t.Errorf("RESTIC_PASSWORD = %q, want from-keychain", gotPw)
	}
}

func TestBuildEnvKeychainMissError(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:        "/backup/repo",
			UseKeychain: true,
		},
	}

	_, err := buildEnv(cfg, "", fakePasswordSource{err: errors.New("not found")})
	if err == nil {
		t.Fatal("buildEnv: nil error, want ErrPasswordFailed")
	}
	if !errors.Is(err, ErrPasswordFailed) {
		t.Errorf("buildEnv error = %v, want errors.Is ErrPasswordFailed", err)
	}
}

// fakePasswordSource is a configurable source for runner tests.
type fakePasswordSource struct {
	pw  string
	err error
}

func (f fakePasswordSource) Get(account string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.pw, nil
}
