package system

import (
	"strings"

	"github.com/spf13/afero"
)

// Environment classifies how javinizer is running so features can adapt:
// docker images cannot self-upgrade (read-only image layers), desktop apps
// are native bundles (.app/.exe/.AppImage) that need a whole-bundle swap, and
// CLI builds are plain binaries that self-replace in place.
type Environment string

const (
	// EnvironmentDocker means the process is inside a container. The image is
	// read-only, so upgrade = `docker pull` + recreate the container, never a
	// self-swap.
	EnvironmentDocker Environment = "docker"
	// EnvironmentDesktop means the build is a clickable native app bundle.
	// Upgrade = download a new bundle (a bare binary swap would orphan the
	// bundle wrapper and lose the embedded icon/Info.plist/.desktop metadata).
	EnvironmentDesktop Environment = "desktop"
	// EnvironmentCLI means a plain binary install (manual/homebrew/scoop). The
	// existing self-upgrade path replaces the binary in place (or hands off
	// to the package manager for brew/scoop).
	EnvironmentCLI Environment = "cli"
)

// dockerImageRef is the published container image. Embedded in the docker
// upgrade instructions so users get a copy-pasteable `docker pull` command
// without having to look it up. Must stay in sync with the image name pushed
// by .github/workflows/cli-release.yml (ghcr.io/javinizer/javinizer-go).
const dockerImageRef = "ghcr.io/javinizer/javinizer-go"

// IsRunningInContainer reports whether the process is inside a Docker (or
// containerd/k8s) container. Detection is best-effort: it checks /.dockerenv
// (the file Docker creates in every container) and /proc/1/cgroup (Linux
// control groups name the container runtime). Extracted from
// internal/scraper/dmm so the upgrade path and the Chrome sandbox logic share
// one source of truth for container detection.
//
// On non-Linux hosts both probes miss and the function returns false, which is
// correct: a macOS/Windows desktop or CLI build is never "in a container".
func IsRunningInContainer(fs afero.Fs) bool {
	if _, err := fs.Stat("/.dockerenv"); err == nil {
		return true
	}
	if data, err := afero.ReadFile(fs, "/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "containerd") ||
			strings.Contains(content, "kubepods") {
			return true
		}
	}
	return false
}

// DetectEnvironment classifies the running build into docker / desktop / cli.
// Desktop is checked first (a cheap ldflags-injected var, passed in by the
// caller as isDesktop so this package stays a leaf with no desktop import):
// the desktop app is a native bundle and is never run inside a container, so
// the docker probe is skipped for it. Otherwise the container probes run; a
// hit means docker, and the default is the plain CLI binary.
//
// The filesystem is injected so tests can simulate /.dockerenv or a cgroup
// file without touching the real root.
func DetectEnvironment(fs afero.Fs, isDesktop bool) Environment {
	if isDesktop {
		return EnvironmentDesktop
	}
	if IsRunningInContainer(fs) {
		return EnvironmentDocker
	}
	return EnvironmentCLI
}

// UpgradeInstructions returns environment-specific guidance for getting the
// latest version. The API surfaces this verbatim in the version status
// response so the Web UI can render the right command without hardcoding the
// image ref or rebuild steps per environment.
//
//   - docker:  `docker pull <image>:latest` then recreate the container
//     (compose users: `docker compose pull && docker compose up -d`).
//   - desktop: download the new bundle from the GitHub releases page; a
//     self-swap would replace only the inner binary and break the bundle.
//   - cli:     run `javinizer upgrade` (or `brew upgrade javinizer` /
//     `scoop update javinizer` for package-managed installs).
func UpgradeInstructions(env Environment) string {
	switch env {
	case EnvironmentDocker:
		return "Running in Docker. Pull the latest image and recreate the container:\n" +
			"  docker pull " + dockerImageRef + ":latest\n" +
			"  # docker compose users: docker compose pull && docker compose up -d"
	case EnvironmentDesktop:
		return "Desktop app: download the new bundle from https://github.com/javinizer/javinizer-go/releases " +
			"and replace your existing app. In-app self-update is not yet supported."
	default:
		return "Run `javinizer upgrade` to update. " +
			"If installed via Homebrew or Scoop, use `brew upgrade javinizer` or `scoop update javinizer` instead."
	}
}
