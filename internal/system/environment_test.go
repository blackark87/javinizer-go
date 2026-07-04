package system

import (
	"testing"

	"github.com/spf13/afero"
)

func TestIsRunningInContainer(t *testing.T) {
	t.Parallel()

	t.Run("dockerenv file present", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if _, err := fs.Create("/.dockerenv"); err != nil {
			t.Fatalf("create /.dockerenv: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when /.dockerenv exists")
		}
	})

	t.Run("containerenv file present (podman/nerdctl)", func(t *testing.T) {
		t.Parallel()
		// Podman and nerdctl create /run/.containerenv instead of /.dockerenv.
		// Without this marker a podman container would fall through to the
		// cgroup substring match, which on cgroup v2 reads just `0::/` and
		// misses — misclassifying the container as CLI and attempting a
		// self-swap the image would lose on recreate.
		fs := afero.NewMemMapFs()
		if _, err := fs.Create("/run/.containerenv"); err != nil {
			t.Fatalf("create /run/.containerenv: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when /run/.containerenv exists")
		}
	})

	t.Run("cgroup mentions docker", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "/proc/1/cgroup", []byte("0::/docker/abcdef123456"), 0o644); err != nil {
			t.Fatalf("write cgroup: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when cgroup mentions docker")
		}
	})

	t.Run("cgroup mentions containerd", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "/proc/1/cgroup", []byte("0::/system.slice/containerd.service"), 0o644); err != nil {
			t.Fatalf("write cgroup: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when cgroup mentions containerd")
		}
	})

	t.Run("cgroup mentions kubepods", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "/proc/1/cgroup", []byte("12:cpuset:/kubepods/podabc"), 0o644); err != nil {
			t.Fatalf("write cgroup: %v", err)
		}
		if !IsRunningInContainer(fs) {
			t.Fatal("expected container detection when cgroup mentions kubepods")
		}
	})

	t.Run("bare host: no dockerenv, no cgroup", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if IsRunningInContainer(fs) {
			t.Fatal("expected no container detection on a bare host")
		}
	})

	t.Run("cgroup without container markers", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if err := afero.WriteFile(fs, "/proc/1/cgroup", []byte("0::/system.slice/sshd.service"), 0o644); err != nil {
			t.Fatalf("write cgroup: %v", err)
		}
		if IsRunningInContainer(fs) {
			t.Fatal("expected no container detection for an ordinary cgroup")
		}
	})
}

func TestDetectEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("desktop short-circuits before docker probe", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if _, err := fs.Create("/.dockerenv"); err != nil {
			t.Fatalf("create /.dockerenv: %v", err)
		}
		got := DetectEnvironment(fs, true)
		if got != EnvironmentDesktop {
			t.Fatalf("DetectEnvironment(isDesktop=true) = %q, want %q", got, EnvironmentDesktop)
		}
	})

	t.Run("cli build in container is docker", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		if _, err := fs.Create("/.dockerenv"); err != nil {
			t.Fatalf("create /.dockerenv: %v", err)
		}
		got := DetectEnvironment(fs, false)
		if got != EnvironmentDocker {
			t.Fatalf("DetectEnvironment(isDesktop=false, in container) = %q, want %q", got, EnvironmentDocker)
		}
	})

	t.Run("cli build on bare host is cli", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		got := DetectEnvironment(fs, false)
		if got != EnvironmentCLI {
			t.Fatalf("DetectEnvironment(isDesktop=false, bare host) = %q, want %q", got, EnvironmentCLI)
		}
	})
}

func TestUpgradeInstructions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  Environment
		want string
	}{
		{"docker mentions docker pull", EnvironmentDocker, "docker pull"},
		{"docker mentions image ref", EnvironmentDocker, "ghcr.io/javinizer/javinizer-go"},
		{"desktop mentions releases", EnvironmentDesktop, "releases"},
		{"cli mentions javinizer upgrade", EnvironmentCLI, "javinizer upgrade"},
		{"cli mentions brew fallback", EnvironmentCLI, "brew upgrade javinizer"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := UpgradeInstructions(tc.env)
			if !contains(got, tc.want) {
				t.Fatalf("UpgradeInstructions(%q) = %q, want substring %q", tc.env, got, tc.want)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
