// Command goqt is the companion tool for the goqt Qt bindings.
//
//	goqt vet [packages]     static ownership and threading checks
//	goqt new app <dir>      write a known-good starter application
package main

import (
	"fmt"
	"os"
)

const usage = `usage:
  goqt vet [packages]        check for ownership and threading mistakes (default ./...)
  goqt new app <dir> [module] write main.go, a window, a behaviour test and a screenshot test
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "vet":
		os.Exit(vet(os.Args[2:]))
	case "new":
		if len(os.Args) < 4 || os.Args[2] != "app" {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		module := ""
		if len(os.Args) > 4 {
			module = os.Args[4]
		}
		if err := newApp(os.Args[3], module); err != nil {
			fmt.Fprintln(os.Stderr, "goqt new:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
