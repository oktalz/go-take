package version

import (
	_ "embed"
)

// Logo is the ASCII art banner displayed at startup.
//
//go:embed logo.txt
var Logo string
