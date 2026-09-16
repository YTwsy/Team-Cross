// Package runtimeconfig defines the creation-time execution policy shared by
// provider runtimes. Trusted inherits the owner's native configuration; it does
// not manufacture unrestricted permissions or bypass native managed policies.
package runtimeconfig

import "fmt"

type Mode string

const (
	Restricted Mode = "restricted"
	Trusted    Mode = "trusted"
)

func Parse(mode Mode) (Mode, error) {
	switch mode {
	case "", Restricted:
		return Restricted, nil
	case Trusted:
		return Trusted, nil
	default:
		return "", fmt.Errorf("不支持的协作模式，请选择受限模式或信任模式")
	}
}
