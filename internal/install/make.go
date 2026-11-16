package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	makeInstallTimeout = 10 * time.Minute
	makeWhereTimeout   = 30 * time.Second
	osReleaseFile      = "/etc/os-release"
)

// makeCommands are the commands that install make with the system package manager, by os-release ID.
var makeCommands = []struct {
	system  string
	ids     []string
	command string
}{
	{"Debian/Ubuntu", []string{"debian", "ubuntu"}, "sudo apt-get install make"},
	{"Fedora/RHEL", []string{"fedora", "rhel", "centos"}, "sudo dnf install make"},
	{"Alpine", []string{"alpine"}, "apk add make"},
	{"Arch", []string{"arch"}, "sudo pacman -S make"},
}

// existingMake returns the make to use when there already is one: on PATH, else an executable dir/make; else "".
func (in *installer) existingMake(dir string) string {
	if path := in.makePath(); path != "" || dir == "" {
		return path
	}
	path := filepath.Join(dir, "make")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return path
	}
	return ""
}

// makeStep installs the pinned make through mise and links it as dir/make. existing is the make already there, ""
// for none; mise is the mise binary, "" when there is none or it did not install.
func (in *installer) makeStep(ctx context.Context, root, dir, existing, mise string, skip bool, stdout, stderr io.Writer) Step {
	step := Step{Name: "make"}
	switch {
	case skip:
		step.Skipped = "skipped on request"
		return step
	case existing != "":
		step.Skipped = "already at " + existing
		return step
	case mise == "":
		step.Err = fmt.Errorf("make is not installed and there is no mise to install it with; %s",
			makeHint(in.goos, in.osRelease()))
		return step
	case dir == "":
		step.Err = errors.New("make is not installed and there is no home directory to link it into")
		return step
	}
	pkg := "make@" + MakeVersion
	env := append(append([]string(nil), in.baseEnv...), "MISE_YES=1")
	step.Command = []string{mise, "install", pkg}
	if err := in.run(ctx, command{dir: root, env: env, argv: step.Command, timeout: makeInstallTimeout}, stdout, stderr); err != nil {
		step.Err = fmt.Errorf("install make: %w", err)
		return step
	}
	var where bytes.Buffer
	whereCmd := command{dir: root, env: env, argv: []string{mise, "where", pkg}, timeout: makeWhereTimeout}
	if err := in.run(ctx, whereCmd, &where, stderr); err != nil {
		step.Err = fmt.Errorf("find the installed make: %w", err)
		return step
	}
	link, target, err := linkMake(dir, strings.TrimSpace(where.String()))
	switch {
	case errors.Is(err, os.ErrExist):
		step.Skipped = "already at " + link
	case err != nil:
		step.Err = err
	default:
		step.Note = "installed " + link + " -> " + target
		if !in.isOnPath(dir) {
			step.Note += fmt.Sprintf("; add %s to PATH so make (and `make check`) works in a shell; pawl finds it there", dir)
		}
	}
	return step
}

// linkMake checks that installDir/bin/make is an executable and links it as dir/make. An existing dir/make is left
// alone: the error then wraps os.ErrExist.
func linkMake(dir, installDir string) (link, target string, err error) {
	link = filepath.Join(dir, "make")
	if installDir == "" {
		return link, "", errors.New("`mise where` printed no directory for make")
	}
	target = filepath.Join(installDir, "bin", "make")
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return link, target, fmt.Errorf("mise installed make, but %s is not an executable file", target)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return link, target, fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return link, target, fmt.Errorf("link %s: %w", link, err)
	}
	return link, target, nil
}

// isOnPath reports whether dir is on the PATH of the process.
func (in *installer) isOnPath(dir string) bool {
	return slices.Contains(filepath.SplitList(in.pathVar), dir)
}

// readOSRelease returns the content of /etc/os-release, or "" when it cannot be read.
func readOSRelease() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	data, err := os.ReadFile(osReleaseFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// makeHint tells how to install make by hand on the platform: goos as runtime.GOOS, osRelease the content of
// /etc/os-release on Linux. The ID is matched before ID_LIKE; an unknown system gets the common commands.
func makeHint(goos, osRelease string) string {
	if goos == "darwin" {
		return "install it with `xcode-select --install`"
	}
	ids := osReleaseIDs(osRelease)
	for _, id := range ids {
		for _, c := range makeCommands {
			if slices.Contains(c.ids, id) {
				return "install it with `" + c.command + "`"
			}
		}
	}
	common := make([]string, 0, len(makeCommands))
	for _, c := range makeCommands {
		common = append(common, c.system+" `"+c.command+"`")
	}
	return "install it with the system package manager: " + strings.Join(common, ", ")
}

// osReleaseIDs returns the ID of the os-release content followed by the words of its ID_LIKE.
func osReleaseIDs(osRelease string) []string {
	var id, like []string
	for _, line := range strings.Split(osRelease, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		words := strings.Fields(strings.ToLower(strings.Trim(value, `"'`)))
		switch key {
		case "ID":
			id = words
		case "ID_LIKE":
			like = words
		}
	}
	return append(id, like...)
}
