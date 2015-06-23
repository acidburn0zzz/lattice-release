package command_factory

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/lattice/ltc/app_examiner"
	"github.com/cloudfoundry-incubator/lattice/ltc/app_runner"
	"github.com/cloudfoundry-incubator/lattice/ltc/app_runner/docker_metadata_fetcher"
	"github.com/cloudfoundry-incubator/lattice/ltc/docker_runner"
	"github.com/cloudfoundry-incubator/lattice/ltc/exit_handler"
	"github.com/cloudfoundry-incubator/lattice/ltc/exit_handler/exit_codes"
	"github.com/cloudfoundry-incubator/lattice/ltc/logs/console_tailed_logs_outputter"
	"github.com/cloudfoundry-incubator/lattice/ltc/terminal"
	"github.com/cloudfoundry-incubator/lattice/ltc/terminal/colors"
	"github.com/codegangsta/cli"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

type pollingAction string

const (
	InvalidPortErrorMessage          = "Invalid port specified. Ports must be a comma-delimited list of integers between 0-65535."
	MalformedRouteErrorMessage       = "Malformed route. Routes must be of the format port:route"
	MustSetMonitoredPortErrorMessage = "Must set monitor-port when specifying multiple exposed ports unless --no-monitor is set."
	MonitorPortNotExposed            = "Must have an exposed port that matches the monitored port"

	DefaultPollingTimeout time.Duration = 2 * time.Minute

	pollingStart pollingAction = "start"
	pollingScale pollingAction = "scale"
)

type DockerRunnerCommandFactory struct {
	appRunner             app_runner.AppRunner
	dockerAppRunner       docker_runner.DockerRunner
	appExaminer           app_examiner.AppExaminer
	ui                    terminal.UI
	dockerMetadataFetcher docker_metadata_fetcher.DockerMetadataFetcher
	domain                string
	env                   []string
	clock                 clock.Clock
	tailedLogsOutputter   console_tailed_logs_outputter.TailedLogsOutputter
	exitHandler           exit_handler.ExitHandler
}

type DockerRunnerCommandFactoryConfig struct {
	AppRunner             app_runner.AppRunner
	DockerRunner          docker_runner.DockerRunner
	AppExaminer           app_examiner.AppExaminer
	UI                    terminal.UI
	DockerMetadataFetcher docker_metadata_fetcher.DockerMetadataFetcher
	Domain                string
	Env                   []string
	Clock                 clock.Clock
	Logger                lager.Logger
	TailedLogsOutputter   console_tailed_logs_outputter.TailedLogsOutputter
	ExitHandler           exit_handler.ExitHandler
}

func NewDockerRunnerCommandFactory(config DockerRunnerCommandFactoryConfig) *DockerRunnerCommandFactory {
	return &DockerRunnerCommandFactory{
		appRunner:       config.AppRunner,
		dockerAppRunner: config.DockerRunner,
		appExaminer:     config.AppExaminer,
		ui:              config.UI,
		dockerMetadataFetcher: config.DockerMetadataFetcher,
		domain:                config.Domain,
		env:                   config.Env,
		clock:                 config.Clock,
		tailedLogsOutputter:   config.TailedLogsOutputter,
		exitHandler:           config.ExitHandler,
	}
}

func (factory *DockerRunnerCommandFactory) MakeCreateAppCommand() cli.Command {

	var createFlags = []cli.Flag{
		cli.StringFlag{
			Name:  "working-dir, w",
			Usage: "Working directory for container (overrides Docker metadata)",
			Value: "",
		},
		cli.BoolFlag{
			Name:  "run-as-root, r",
			Usage: "Runs in the context of the root user",
		},
		cli.StringSliceFlag{
			Name:  "env, e",
			Usage: "Environment variables (can be passed multiple times)",
			Value: &cli.StringSlice{},
		},
		cli.IntFlag{
			Name:  "cpu-weight, c",
			Usage: "Relative CPU weight for the container (valid values: 1-100)",
			Value: 100,
		},
		cli.IntFlag{
			Name:  "memory-mb, m",
			Usage: "Memory limit for container in MB",
			Value: 128,
		},
		cli.IntFlag{
			Name:  "disk-mb, d",
			Usage: "Disk limit for container in MB",
			Value: 0,
		},
		cli.StringFlag{
			Name:  "ports, p",
			Usage: "Ports to expose on the container (comma delimited)",
		},
		cli.IntFlag{
			Name:  "monitor-port, M",
			Usage: "Selects the port used to healthcheck the app",
		},
		cli.StringFlag{
			Name: "monitor-url, U",
			Usage: "Uses HTTP to healthcheck the app\n\t\t" +
				"format is: port:/path/to/endpoint",
		},
		cli.DurationFlag{
			Name:  "monitor-timeout",
			Usage: "Timeout for the app healthcheck",
			Value: time.Second,
		},
		cli.StringFlag{
			Name: "routes, R",
			Usage: "Route mappings to exposed ports as follows:\n\t\t" +
				"--routes=80:web,8080:api will route web to 80 and api to 8080",
		},
		cli.IntFlag{
			Name:  "instances, i",
			Usage: "Number of application instances to spawn on launch",
			Value: 1,
		},
		cli.BoolFlag{
			Name:  "no-monitor",
			Usage: "Disables healthchecking for the app",
		},
		cli.BoolFlag{
			Name:  "no-routes",
			Usage: "Registers no routes for the app",
		},
		cli.DurationFlag{
			Name:  "timeout, t",
			Usage: "Polling timeout for app to start",
			Value: DefaultPollingTimeout,
		},
	}

	var createAppCommand = cli.Command{
		Name:    "create",
		Aliases: []string{"cr"},
		Usage:   "Creates a docker app on lattice",
		Description: `ltc create APP_NAME DOCKER_IMAGE

   APP_NAME is required and must be unique across the Lattice cluster
   DOCKER_IMAGE is required and must match the standard docker image format
   e.g.
   		1. "cloudfoundry/lattice-app"
   		2. "redis" - for official images; resolves to library/redis

   ltc will fetch the command associated with your Docker image.
   To provide a custom command:
   ltc create APP_NAME DOCKER_IMAGE <optional flags> -- START_COMMAND APP_ARG1 APP_ARG2 ...

   ltc will also fetch the working directory associated with your Docker image.
   If the image does not specify a working directory, ltc will default the working directory to "/"
   To provide a custom working directory:
   ltc create APP_NAME DOCKER_IMAGE --working-dir=/foo/app-folder -- START_COMMAND APP_ARG1 APP_ARG2 ...

   To specify environment variables:
   ltc create APP_NAME DOCKER_IMAGE -e FOO=BAR -e BAZ=WIBBLE
`,
		Action: factory.createApp,
		Flags:  createFlags,
	}

	return createAppCommand
}

func (factory *DockerRunnerCommandFactory) createApp(context *cli.Context) {
	workingDirFlag := context.String("working-dir")
	envVarsFlag := context.StringSlice("env")
	instancesFlag := context.Int("instances")
	cpuWeightFlag := uint(context.Int("cpu-weight"))
	memoryMBFlag := context.Int("memory-mb")
	diskMBFlag := context.Int("disk-mb")
	portsFlag := context.String("ports")
	noMonitorFlag := context.Bool("no-monitor")
	portMonitorFlag := context.Int("monitor-port")
	urlMonitorFlag := context.String("monitor-url")
	monitorTimeoutFlag := context.Duration("monitor-timeout")
	routesFlag := context.String("routes")
	noRoutesFlag := context.Bool("no-routes")
	timeoutFlag := context.Duration("timeout")
	name := context.Args().Get(0)
	dockerImage := context.Args().Get(1)
	terminator := context.Args().Get(2)
	startCommand := context.Args().Get(3)

	var appArgs []string

	switch {
	case len(context.Args()) < 2:
		factory.ui.SayIncorrectUsage("APP_NAME and DOCKER_IMAGE are required")
		factory.exitHandler.Exit(exit_codes.InvalidSyntax)
		return
	case startCommand != "" && terminator != "--":
		factory.ui.SayIncorrectUsage("'--' Required before start command")
		factory.exitHandler.Exit(exit_codes.InvalidSyntax)
		return
	case len(context.Args()) > 4:
		appArgs = context.Args()[4:]
	case cpuWeightFlag < 1 || cpuWeightFlag > 100:
		factory.ui.SayIncorrectUsage("Invalid CPU Weight")
		factory.exitHandler.Exit(exit_codes.InvalidSyntax)
		return
	}

	imageMetadata, err := factory.dockerMetadataFetcher.FetchMetadata(dockerImage)
	if err != nil {
		factory.ui.Say(fmt.Sprintf("Error fetching image metadata: %s", err))
		factory.exitHandler.Exit(exit_codes.BadDocker)
		return
	}

	exposedPorts, err := factory.getExposedPortsFromArgs(portsFlag, imageMetadata)
	if err != nil {
		factory.ui.Say(err.Error())
		factory.exitHandler.Exit(exit_codes.InvalidSyntax)
		return
	}

	monitorConfig, err := factory.getMonitorConfigFromArgs(exposedPorts, portMonitorFlag, noMonitorFlag, urlMonitorFlag, monitorTimeoutFlag, imageMetadata)
	if err != nil {
		factory.ui.Say(err.Error())
		if err.Error() == MonitorPortNotExposed {
			factory.exitHandler.Exit(exit_codes.CommandFailed)
		} else {
			factory.exitHandler.Exit(exit_codes.InvalidSyntax)
		}
		return
	}

	if workingDirFlag == "" {
		factory.ui.Say("No working directory specified, using working directory from the image metadata...\n")
		if imageMetadata.WorkingDir != "" {
			workingDirFlag = imageMetadata.WorkingDir
			factory.ui.Say("Working directory is:\n")
			factory.ui.Say(workingDirFlag + "\n")
		} else {
			workingDirFlag = "/"
		}
	}

	if !noMonitorFlag {
		factory.ui.Say(fmt.Sprintf("Monitoring the app on port %d...\n", monitorConfig.Port))
	} else {
		factory.ui.Say("No ports will be monitored.\n")
	}

	if startCommand == "" {
		if len(imageMetadata.StartCommand) == 0 {
			factory.ui.SayLine("Unable to determine start command from image metadata.")
			factory.exitHandler.Exit(exit_codes.BadDocker)
			return
		}

		factory.ui.Say("No start command specified, using start command from the image metadata...\n")
		startCommand = imageMetadata.StartCommand[0]

		factory.ui.Say("Start command is:\n")
		factory.ui.Say(strings.Join(imageMetadata.StartCommand, " ") + "\n")

		appArgs = imageMetadata.StartCommand[1:]
	}

	routeOverrides, err := parseRouteOverrides(routesFlag)
	if err != nil {
		factory.ui.Say(err.Error())
		factory.exitHandler.Exit(exit_codes.InvalidSyntax)
		return
	}

	err = factory.dockerAppRunner.CreateDockerApp(app_runner.CreateAppParams{
		Name:                 name,
		RootFS:               dockerImage,
		StartCommand:         startCommand,
		AppArgs:              appArgs,
		EnvironmentVariables: factory.buildEnvironment(envVarsFlag, name),
		Privileged:           context.Bool("run-as-root"),
		Monitor:              monitorConfig,
		Instances:            instancesFlag,
		CPUWeight:            cpuWeightFlag,
		MemoryMB:             memoryMBFlag,
		DiskMB:               diskMBFlag,
		ExposedPorts:         exposedPorts,
		WorkingDir:           workingDirFlag,
		RouteOverrides:       routeOverrides,
		NoRoutes:             noRoutesFlag,
		Timeout:              timeoutFlag,
	})
	if err != nil {
		factory.ui.Say(fmt.Sprintf("Error creating app: %s", err))
		factory.exitHandler.Exit(exit_codes.CommandFailed)
		return
	}

	factory.ui.Say("Creating App: " + name + "\n")

	go factory.tailedLogsOutputter.OutputTailedLogs(name)
	defer factory.tailedLogsOutputter.StopOutputting()

	ok := factory.pollUntilAllInstancesRunning(timeoutFlag, name, instancesFlag, "start")

	if noRoutesFlag {
		factory.ui.Say(colors.Green(name + " is now running.\n"))
		return
	} else if ok {
		factory.ui.Say(colors.Green(name + " is now running.\n"))
		factory.ui.Say("App is reachable at:\n")
	} else {
		factory.ui.Say("App will be reachable at:\n")
	}

	if routeOverrides != nil {
		for _, route := range strings.Split(routesFlag, ",") {
			factory.ui.Say(colors.Green(factory.urlForApp(strings.Split(route, ":")[1])))
		}
	} else {
		factory.ui.Say(colors.Green(factory.urlForApp(name)))
	}
}

func (factory *DockerRunnerCommandFactory) pollUntilSuccess(pollTimeout time.Duration, pollingFunc func() bool, outputProgress bool) (ok bool) {
	startingTime := factory.clock.Now()
	for startingTime.Add(pollTimeout).After(factory.clock.Now()) {
		if result := pollingFunc(); result {
			factory.ui.SayNewLine()
			return true
		} else if outputProgress {
			factory.ui.Say(".")
		}

		factory.clock.Sleep(1 * time.Second)
	}
	factory.ui.SayNewLine()
	return false
}

func (factory *DockerRunnerCommandFactory) pollUntilAllInstancesRunning(pollTimeout time.Duration, appName string, instances int, action pollingAction) bool {
	placementErrorOccurred := false
	ok := factory.pollUntilSuccess(pollTimeout, func() bool {
		numberOfRunningInstances, placementError, _ := factory.appExaminer.RunningAppInstancesInfo(appName)
		if placementError {
			factory.ui.Say(colors.Red("Error, could not place all instances: insufficient resources. Try requesting fewer instances or reducing the requested memory or disk capacity."))
			placementErrorOccurred = true
			return true
		}
		return numberOfRunningInstances == instances
	}, true)

	if placementErrorOccurred {
		factory.exitHandler.Exit(exit_codes.PlacementError)
		return false
	} else if !ok {
		if action == pollingStart {
			factory.ui.Say(colors.Red("Timed out waiting for the container to come up."))
			factory.ui.SayNewLine()
			factory.ui.SayLine("This typically happens because docker layers can take time to download.")
			factory.ui.SayLine("Lattice is still downloading your application in the background.")
		} else {
			factory.ui.Say(colors.Red("Timed out waiting for the container to scale."))
			factory.ui.SayNewLine()
			factory.ui.SayLine("Lattice is still scaling your application in the background.")
		}
		factory.ui.SayLine(fmt.Sprintf("To view logs:\n\tltc logs %s", appName))
		factory.ui.SayLine(fmt.Sprintf("To view status:\n\tltc status %s", appName))
		factory.ui.SayNewLine()
	}
	return ok
}

func (factory *DockerRunnerCommandFactory) urlForApp(name string) string {
	return fmt.Sprintf("http://%s.%s\n", name, factory.domain)
}

func (factory *DockerRunnerCommandFactory) buildEnvironment(envVars []string, appName string) map[string]string {
	environment := make(map[string]string)
	environment["PROCESS_GUID"] = appName

	for _, envVarPair := range envVars {
		name, value := parseEnvVarPair(envVarPair)

		if value == "" {
			value = factory.grabVarFromEnv(name)
		}

		environment[name] = value
	}
	return environment
}

func (factory *DockerRunnerCommandFactory) grabVarFromEnv(name string) string {
	for _, envVarPair := range factory.env {
		if strings.HasPrefix(envVarPair, name) {
			_, value := parseEnvVarPair(envVarPair)
			return value
		}
	}
	return ""
}

func (factory *DockerRunnerCommandFactory) getExposedPortsFromArgs(portsFlag string, imageMetadata *docker_metadata_fetcher.ImageMetadata) ([]uint16, error) {
	if portsFlag != "" {
		portStrings := strings.Split(portsFlag, ",")
		sort.Strings(portStrings)

		convertedPorts := []uint16{}
		for _, p := range portStrings {
			intPort, err := strconv.Atoi(p)
			if err != nil || intPort > 65535 {
				return []uint16{}, errors.New(InvalidPortErrorMessage)
			}
			convertedPorts = append(convertedPorts, uint16(intPort))
		}
		return convertedPorts, nil
	}

	if len(imageMetadata.ExposedPorts) > 0 {
		var exposedPortStrings []string
		for _, port := range imageMetadata.ExposedPorts {
			exposedPortStrings = append(exposedPortStrings, strconv.Itoa(int(port)))
		}
		factory.ui.Say(fmt.Sprintf("No port specified, using exposed ports from the image metadata.\n\tExposed Ports: %s\n", strings.Join(exposedPortStrings, ", ")))
		return imageMetadata.ExposedPorts, nil
	}

	factory.ui.Say(fmt.Sprintf("No port specified, image metadata did not contain exposed ports. Defaulting to 8080.\n"))
	return []uint16{8080}, nil
}

func (factory *DockerRunnerCommandFactory) getMonitorConfigFromArgs(exposedPorts []uint16, portMonitorFlag int, noMonitorFlag bool, urlMonitorFlag string, monitorTimeoutFlag time.Duration, imageMetadata *docker_metadata_fetcher.ImageMetadata) (app_runner.MonitorConfig, error) {
	if noMonitorFlag {
		return app_runner.MonitorConfig{
			Method: app_runner.NoMonitor,
		}, nil
	}

	if urlMonitorFlag != "" {
		urlMonitorArr := strings.Split(urlMonitorFlag, ":")
		if len(urlMonitorArr) != 2 {
			return app_runner.MonitorConfig{}, errors.New(InvalidPortErrorMessage)
		}

		urlMonitorPort, err := strconv.Atoi(urlMonitorArr[0])
		if err != nil {
			return app_runner.MonitorConfig{}, errors.New(InvalidPortErrorMessage)
		}

		if err := checkPortExposed(exposedPorts, uint16(urlMonitorPort)); err != nil {
			return app_runner.MonitorConfig{}, err
		}

		return app_runner.MonitorConfig{
			Method:  app_runner.URLMonitor,
			Port:    uint16(urlMonitorPort),
			URI:     urlMonitorArr[1],
			Timeout: monitorTimeoutFlag,
		}, nil
	}

	var sortedPorts []int
	for _, port := range exposedPorts {
		sortedPorts = append(sortedPorts, int(port))
	}
	sort.Ints(sortedPorts)

	// Unsafe array access:  because we'll default exposing 8080 if
	// both --ports is empty and docker image has no EXPOSE ports
	monitorPort := uint16(sortedPorts[0])
	if portMonitorFlag > 0 {
		monitorPort = uint16(portMonitorFlag)
	}

	if err := checkPortExposed(exposedPorts, monitorPort); err != nil {
		return app_runner.MonitorConfig{}, err
	}

	return app_runner.MonitorConfig{
		Method:  app_runner.PortMonitor,
		Port:    uint16(monitorPort),
		Timeout: monitorTimeoutFlag,
	}, nil
}

func checkPortExposed(exposedPorts []uint16, monitorPort uint16) error {
	portFound := false
	for _, port := range exposedPorts {
		if port == uint16(monitorPort) {
			portFound = true
			break
		}
	}
	if !portFound {
		return errors.New(MonitorPortNotExposed)
	}

	return nil
}

func parseRouteOverrides(routes string) (app_runner.RouteOverrides, error) {
	var routeOverrides app_runner.RouteOverrides

	for _, route := range strings.Split(routes, ",") {
		if route == "" {
			continue
		}
		routeArr := strings.Split(route, ":")
		maybePort, err := strconv.Atoi(routeArr[0])
		if err != nil || len(routeArr) < 2 {
			return nil, errors.New(MalformedRouteErrorMessage)
		}

		port := uint16(maybePort)
		hostnamePrefix := routeArr[1]
		routeOverrides = append(routeOverrides, app_runner.RouteOverride{HostnamePrefix: hostnamePrefix, Port: port})
	}

	return routeOverrides, nil
}

func parseEnvVarPair(envVarPair string) (name, value string) {
	s := strings.SplitN(envVarPair, "=", 2)
	if len(s) > 1 {
		return s[0], s[1]
	}
	return s[0], ""
}