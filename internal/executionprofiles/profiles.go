package executionprofiles

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Profile struct {
	Name                 string
	Executable           string
	ArgumentPrefix       []string
	AllowedEnvironment   map[string]string
	AllowArguments       func([]string) error
	TimeoutCeiling       time.Duration
	RequiresUnprivileged bool
}

type Registry struct{ profiles map[string]Profile }

func DefaultRegistry() Registry {
	pythonArg := regexp.MustCompile(`^[A-Za-z0-9_./\\:-]+$`)
	return New([]Profile{
		{Name: "codex-safe", Executable: "codex", ArgumentPrefix: []string{"exec", "--sandbox", "workspace-write"}, AllowedEnvironment: map[string]string{}, AllowArguments: rejectAll, TimeoutCeiling: 20 * time.Minute, RequiresUnprivileged: true},
		{Name: "python-unittest", Executable: "python", ArgumentPrefix: []string{"-B", "-m", "unittest"}, AllowedEnvironment: map[string]string{"PYTHONDONTWRITEBYTECODE": "1"}, AllowArguments: func(args []string) error {
			for _, arg := range args {
				if !pythonArg.MatchString(arg) || strings.Contains(arg, "..") {
					return fmt.Errorf("unsafe unittest argument %q", arg)
				}
			}
			return nil
		}, TimeoutCeiling: 20 * time.Minute, RequiresUnprivileged: true},
		{Name: "go-test", Executable: "go", ArgumentPrefix: []string{"test"}, AllowedEnvironment: map[string]string{}, AllowArguments: func(args []string) error {
			for _, arg := range args {
				if arg != "./..." && arg != "-race" && arg != "-count=1" {
					return fmt.Errorf("unsupported go-test argument %q", arg)
				}
			}
			return nil
		}, TimeoutCeiling: 30 * time.Minute, RequiresUnprivileged: true},
	})
}

func New(profiles []Profile) Registry {
	registry := Registry{profiles: map[string]Profile{}}
	for _, profile := range profiles {
		registry.profiles[profile.Name] = profile
	}
	return registry
}
func (registry Registry) Resolve(name string, args []string, requestedTimeout time.Duration) (Profile, []string, error) {
	profile, ok := registry.profiles[name]
	if !ok {
		return Profile{}, nil, fmt.Errorf("unknown execution profile %q", name)
	}
	if requestedTimeout <= 0 || requestedTimeout > profile.TimeoutCeiling {
		return Profile{}, nil, fmt.Errorf("timeout exceeds %s profile ceiling", name)
	}
	if profile.AllowArguments == nil {
		return Profile{}, nil, errors.New("profile has no argument policy")
	}
	if err := profile.AllowArguments(args); err != nil {
		return Profile{}, nil, err
	}
	resolved := append([]string{}, profile.ArgumentPrefix...)
	resolved = append(resolved, args...)
	return profile, resolved, nil
}
func rejectAll(args []string) error {
	if len(args) > 0 {
		return errors.New("profile does not accept coordinator arguments")
	}
	return nil
}
