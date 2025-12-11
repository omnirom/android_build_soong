package android

import (
	"github.com/google/blueprint"
)

type ModuleProxy struct {
	blueprint.ModuleProxy
}

type ModuleOrProxy interface {
	blueprint.ModuleOrProxy
}

func CreateModuleProxy(module Module) ModuleProxy {
	return ModuleProxy{blueprint.CreateModuleProxy(module)}
}
