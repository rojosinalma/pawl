package install

// The mise release pawl installs when the machine has none, and the SHA-256 of each archive it may download.
//
// To bump: set miseVersion to the new tag, then replace the four sums with the lines for
// mise-<tag>-{linux,macos}-{x64,arm64}.tar.gz of
//
//	curl -fsSL https://github.com/jdx/mise/releases/download/<tag>/SHASUMS256.txt
const (
	miseVersion = "v2026.11.9"

	miseSHA256LinuxX64   = "23ba592fcbc61f057e2b82c1eafd085832712ba0c759b714ca16cd06babca378"
	miseSHA256LinuxArm64 = "cf2900bfb4279f8d8f8a19d5c27bf1bde826b3e742182f3f67511c588498c786"
	miseSHA256MacosX64   = "21288d7a2d82066f1a2ce83c48b7b4b57753edd739b6309e578aafce0edc8a03"
	miseSHA256MacosArm64 = "e4509f159e0492e89065edcf52f2918e2751e3574620586058468ba0563aed59"
)

// MakeVersion is the GNU make pawl installs through mise when the machine has none. mise's registry maps `make` to
// the conda backend (conda-forge builds), which verifies the package it downloads; pawl pins only the version.
//
// To bump: pick a version listed by `mise ls-remote make` (the make package of conda-forge), set it here and check
// that `mise install make@<version>` gives a `bin/make` that runs on a clean Linux and macOS.
const MakeVersion = "4.4.1"

// pinnedSHA256 returns the sums of the pinned release by platform ("linux-x64").
func pinnedSHA256() map[string]string {
	return map[string]string{
		"linux-x64":   miseSHA256LinuxX64,
		"linux-arm64": miseSHA256LinuxArm64,
		"macos-x64":   miseSHA256MacosX64,
		"macos-arm64": miseSHA256MacosArm64,
	}
}
