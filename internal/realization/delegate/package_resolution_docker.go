package delegate

import (
	"strconv"
	"strings"
)

// Docker global options precede the run subcommand; run options precede the
// image. Unknown options preserve argv but make package resolution incomplete.
// https://docs.docker.com/reference/cli/docker/
// https://docs.docker.com/reference/cli/docker/container/run/
func dockerImageSpec(args []string) (string, bool, bool, error) {
	start, complete, err := dockerRunArgumentsStart(args)
	if err != nil {
		return "", false, false, err
	}
	if !complete {
		return "", false, false, nil
	}
	if start < 0 {
		return "", false, false, validationError(
			ReasonInvalidDelegatePlan,
			string(RunnerDocker),
			"docker delegate requires the run or container run subcommand",
		)
	}
	for index := start; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if index+1 >= len(args) {
				return "", false, true, nil
			}
			return args[index+1], true, true, nil
		}
		if strings.HasPrefix(arg, "--") {
			if option, value, assigned := strings.Cut(arg, "="); assigned {
				if dockerLongOptionTakesValue(option) {
					continue
				}
				if dockerLongFlagOption(option) && dockerBooleanValue(value) {
					continue
				}
				return "", false, false, nil
			}
			if dockerLongOptionTakesValue(arg) {
				if _, err := requiredSeparateOptionValue(args, index, RunnerDocker); err != nil {
					return "", false, false, err
				}
				index++
				continue
			}
			if dockerLongFlagOption(arg) {
				continue
			}
			return "", false, false, nil
		}
		if strings.HasPrefix(arg, "-") {
			consumesNext, known := dockerShortOptionArity(arg)
			if !known {
				return "", false, false, nil
			}
			if consumesNext {
				if _, err := requiredSeparateOptionValue(args, index, RunnerDocker); err != nil {
					return "", false, false, err
				}
				index++
			}
			continue
		}
		return arg, true, true, nil
	}
	return "", false, true, nil
}

func dockerRunArgumentsStart(args []string) (int, bool, error) {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "run":
			return index + 1, true, nil
		case arg == "container":
			if index+1 < len(args) && args[index+1] == "run" {
				return index + 2, true, nil
			}
			return -1, true, nil
		case arg == "--":
			if index+1 >= len(args) {
				return -1, true, nil
			}
			if args[index+1] == "run" {
				return index + 2, true, nil
			}
			if index+2 < len(args) && args[index+1] == "container" && args[index+2] == "run" {
				return index + 3, true, nil
			}
			return -1, true, nil
		case strings.HasPrefix(arg, "--"):
			if before, value, found := strings.Cut(arg, "="); found {
				if dockerGlobalOptionTakesValue(before) {
					continue
				}
				if dockerGlobalFlagOption(before) && dockerBooleanValue(value) {
					continue
				}
				return -1, false, nil
			}
			if dockerGlobalOptionTakesValue(arg) {
				if _, err := requiredSeparateOptionValue(args, index, RunnerDocker); err != nil {
					return -1, false, err
				}
				index++
				continue
			}
			if dockerGlobalFlagOption(arg) {
				continue
			}
			return -1, false, nil
		case strings.HasPrefix(arg, "-"):
			consumesNext, known := dockerGlobalShortOptionArity(arg)
			if !known {
				return -1, false, nil
			}
			if consumesNext {
				if _, err := requiredSeparateOptionValue(args, index, RunnerDocker); err != nil {
					return -1, false, err
				}
				index++
			}
		default:
			return -1, true, nil
		}
	}
	return -1, true, nil
}

func splitDockerSpec(spec string) (string, string, error) {
	if strings.HasSuffix(spec, "@") || strings.HasSuffix(spec, ":") {
		return "", "", validationError(ReasonInvalidPackageRef, spec, "container image selector is empty")
	}
	if selectorIndex := strings.LastIndex(spec, "@"); selectorIndex > 0 {
		return spec[:selectorIndex], spec[selectorIndex+1:], nil
	}
	lastSlash := strings.LastIndex(spec, "/")
	lastColon := strings.LastIndex(spec, ":")
	if lastColon > lastSlash {
		return spec[:lastColon], spec[lastColon+1:], nil
	}
	return spec, "", nil
}

func dockerLongOptionTakesValue(option string) bool {
	switch option {
	case "--add-host", "--annotation", "--attach", "--blkio-weight", "--blkio-weight-device",
		"--cap-add", "--cap-drop", "--cgroup-parent", "--cgroupns", "--cidfile", "--cpu-count",
		"--cpu-percent", "--cpu-period", "--cpu-quota", "--cpu-rt-period", "--cpu-rt-runtime",
		"--cpu-shares", "--cpus", "--cpuset-cpus", "--cpuset-mems", "--detach-keys", "--device",
		"--device-cgroup-rule", "--device-read-bps", "--device-read-iops", "--device-write-bps",
		"--device-write-iops", "--dns", "--dns-option", "--dns-search", "--domainname",
		"--entrypoint", "--env", "--env-file", "--expose", "--gpus", "--group-add", "--health-cmd",
		"--health-interval", "--health-retries", "--health-start-interval", "--health-start-period",
		"--health-timeout", "--hostname", "--io-maxbandwidth", "--io-maxiops", "--ip", "--ip6",
		"--ipc", "--isolation", "--label", "--label-file", "--link", "--link-local-ip",
		"--log-driver", "--log-opt", "--mac-address", "--memory", "--memory-reservation",
		"--memory-swap", "--memory-swappiness", "--mount", "--name", "--network", "--network-alias",
		"--oom-score-adj", "--pid", "--pids-limit", "--platform", "--publish", "--pull", "--restart",
		"--runtime", "--security-opt", "--shm-size", "--stop-signal", "--stop-timeout",
		"--storage-opt", "--sysctl", "--tmpfs", "--ulimit", "--user", "--userns", "--uts",
		"--volume", "--volume-driver", "--volumes-from", "--workdir":
		return true
	default:
		return false
	}
}

func dockerLongFlagOption(option string) bool {
	switch option {
	case "--detach", "--help", "--init", "--interactive", "--no-healthcheck", "--oom-kill-disable",
		"--privileged", "--publish-all", "--quiet", "--read-only", "--rm", "--sig-proxy", "--tty",
		"--use-api-socket":
		return true
	}
	return false
}

func dockerBooleanValue(value string) bool {
	_, err := strconv.ParseBool(value)
	return err == nil
}

func dockerGlobalOptionTakesValue(option string) bool {
	switch option {
	case "--config", "--context", "--host", "--log-level", "--tlscacert", "--tlscert", "--tlskey":
		return true
	default:
		return false
	}
}

func dockerGlobalFlagOption(option string) bool {
	switch option {
	case "--debug", "--tls", "--tlsverify":
		return true
	default:
		return false
	}
}

func dockerGlobalShortOptionArity(option string) (bool, bool) {
	if len(option) < 2 || option[0] != '-' || option[1] == '-' {
		return false, false
	}
	if option == "-D" {
		return false, true
	}
	if strings.ContainsRune("cHl", rune(option[1])) {
		return len(option) == 2, true
	}
	return false, false
}

func dockerShortOptionArity(option string) (bool, bool) {
	if len(option) < 2 || option[0] != '-' || option[1] == '-' {
		return false, false
	}
	valueOptions := "acehlmpuvw"
	flagOptions := "diPqt"
	first := option[1]
	if strings.ContainsRune(valueOptions, rune(first)) {
		return len(option) == 2, true
	}
	for _, value := range option[1:] {
		if !strings.ContainsRune(flagOptions, value) {
			return false, false
		}
	}
	return false, true
}
