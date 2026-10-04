package mcpserver

import "github.com/aiveto/veto/capability"

type (
	SearchArgs   = capability.SearchArgs
	DescribeArgs = capability.DescribeArgs
	InvokeArgs   = capability.InvokeArgs
	Capability   = capability.Capability
	Line         = capability.Line
)

const (
	SearchName          = capability.SearchName
	SearchCommand       = capability.SearchCommand
	SearchDescription   = capability.SearchDescription
	DescribeName        = capability.DescribeName
	DescribeCommand     = capability.DescribeCommand
	DescribeDescription = capability.DescribeDescription
	InvokeName          = capability.InvokeName
	InvokeCommand       = capability.InvokeCommand
	InvokeDescription   = capability.InvokeDescription
)

func Capabilities() []capability.Capability {
	return capability.All()
}

func CapabilityByCommand(name string) (capability.Capability, bool) {
	return capability.ByCommand(name)
}
