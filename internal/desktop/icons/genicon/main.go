// genicon generates a Windows .syso COFF resource from a .ico file, embedding
// the icon as RT_GROUP_ICON at resource ID 3 — the ID Wails' window manager
// loads via AppIconID (see wails v2 .../windows/winc/app.go: AppIconID = 3).
//
// The `rsrc -ico` CLI assigns resource ID 1, which makes the .exe/taskbar icon
// render (Explorer picks the lowest-ID icon) but leaves the in-app window
// title-bar icon generic, because Wails requests ID 3 and finds nothing. This
// tool mirrors the Wails CLI's own packager (which uses tc-hib/winres's
// SetIcon(RT_ICON, ...), reusing RT_ICON's value 3 as the group-icon ID) so
// both the executable icon and the window icon resolve.
//
// Pure Go: runs anywhere via `go run`, so the Makefile (macOS cross-compile)
// and the release workflow (windows-latest) share one mechanism with no system
// binary dependency.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tc-hib/winres"
)

func main() {
	var (
		icoPath = flag.String("ico", "", "path to the .ico file")
		outPath = flag.String("o", "", "output .syso path")
		arch    = flag.String("arch", "amd64", "target architecture: 386, amd64, arm64")
	)
	flag.Parse()

	if *icoPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: genicon -ico FILE.ico -o FILE.syso [-arch amd64]")
		os.Exit(2)
	}

	f, err := os.Open(*icoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genicon: open ico: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }() // genicon exits on any read error; close result is immaterial

	icon, err := winres.LoadICO(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genicon: load ico: %v\n", err)
		os.Exit(1)
	}

	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.RT_ICON, icon); err != nil {
		fmt.Fprintf(os.Stderr, "genicon: set icon: %v\n", err)
		os.Exit(1)
	}

	archs := map[string]winres.Arch{
		"386":   winres.ArchI386,
		"amd64": winres.ArchAMD64,
		"arm64": winres.ArchARM64,
	}
	targetArch, ok := archs[*arch]
	if !ok {
		fmt.Fprintf(os.Stderr, "genicon: unsupported arch %q (want 386, amd64, or arm64)\n", *arch)
		os.Exit(2)
	}

	out, err := os.Create(*outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genicon: create output: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = out.Close() }() // best-effort: WriteObject already flushed

	if err := rs.WriteObject(out, targetArch); err != nil {
		fmt.Fprintf(os.Stderr, "genicon: write syso: %v\n", err)
		os.Exit(1)
	}
}
