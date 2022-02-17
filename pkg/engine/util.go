package engine

import (
	"fmt"
)

func EngineMethod(path string, method string) {
	switch method {
	case "raw":
		RawPodman(path)
	case "kube":
		fmt.Printf("TBD")
	}
}
