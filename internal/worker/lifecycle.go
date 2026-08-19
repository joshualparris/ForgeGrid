package worker

type Lifecycle interface {
	Mode() string
	Start(tx *UpdateTransaction) error
}

func GetLifecycle(mode string) Lifecycle {
	switch mode {
	case "windows-service":
		return newWindowsServiceLifecycle()
	case "systemd":
		return newSystemdLifecycle()
	default:
		return newPortableLifecycle()
	}
}

func DetectCurrentLifecycle() string {
	return detectLifecycleOS()
}
