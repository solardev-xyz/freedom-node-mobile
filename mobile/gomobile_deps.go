package mobile

// gobind needs golang.org/x/mobile/bind to be resolvable as a real package
// in the target's module graph (it analyzes the package tree via
// go/packages to emit Java/ObjC stubs). Without this blank import the
// package is only pulled in transitively via go.sum, which gobind rejects
// with "no Go package in golang.org/x/mobile/bind". Keep this line.
import _ "golang.org/x/mobile/bind"
