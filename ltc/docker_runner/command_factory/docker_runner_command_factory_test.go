package command_factory_test

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"github.com/cloudfoundry-incubator/lattice/ltc/app_examiner/fake_app_examiner"
	"github.com/cloudfoundry-incubator/lattice/ltc/app_runner"
	"github.com/cloudfoundry-incubator/lattice/ltc/app_runner/docker_metadata_fetcher"
	"github.com/cloudfoundry-incubator/lattice/ltc/app_runner/docker_metadata_fetcher/fake_docker_metadata_fetcher"
	"github.com/cloudfoundry-incubator/lattice/ltc/app_runner/fake_app_runner"
	"github.com/cloudfoundry-incubator/lattice/ltc/docker_runner/command_factory"
	"github.com/cloudfoundry-incubator/lattice/ltc/docker_runner/fake_docker_runner"
	"github.com/cloudfoundry-incubator/lattice/ltc/exit_handler/exit_codes"
	"github.com/cloudfoundry-incubator/lattice/ltc/exit_handler/fake_exit_handler"
	"github.com/cloudfoundry-incubator/lattice/ltc/logs/console_tailed_logs_outputter/fake_tailed_logs_outputter"
	"github.com/cloudfoundry-incubator/lattice/ltc/terminal"
	"github.com/cloudfoundry-incubator/lattice/ltc/terminal/colors"
	"github.com/cloudfoundry-incubator/lattice/ltc/test_helpers"
	. "github.com/cloudfoundry-incubator/lattice/ltc/test_helpers/matchers"
	"github.com/codegangsta/cli"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager"
)

var _ = Describe("CommandFactory", func() {

	var (
		appRunner                     *fake_app_runner.FakeAppRunner
		dockerRunner                  *fake_docker_runner.FakeDockerRunner
		appExaminer                   *fake_app_examiner.FakeAppExaminer
		outputBuffer                  *gbytes.Buffer
		terminalUI                    terminal.UI
		domain                        string = "192.168.11.11.xip.io"
		clock                         *fakeclock.FakeClock
		dockerMetadataFetcher         *fake_docker_metadata_fetcher.FakeDockerMetadataFetcher
		appRunnerCommandFactoryConfig command_factory.DockerRunnerCommandFactoryConfig
		logger                        lager.Logger
		fakeTailedLogsOutputter       *fake_tailed_logs_outputter.FakeTailedLogsOutputter
		fakeExitHandler               *fake_exit_handler.FakeExitHandler
	)

	BeforeEach(func() {
		appRunner = &fake_app_runner.FakeAppRunner{}
		dockerRunner = &fake_docker_runner.FakeDockerRunner{}
		appExaminer = &fake_app_examiner.FakeAppExaminer{}
		outputBuffer = gbytes.NewBuffer()
		terminalUI = terminal.NewUI(nil, outputBuffer, nil)
		dockerMetadataFetcher = &fake_docker_metadata_fetcher.FakeDockerMetadataFetcher{}
		clock = fakeclock.NewFakeClock(time.Now())
		logger = lager.NewLogger("ltc-test")
		fakeTailedLogsOutputter = fake_tailed_logs_outputter.NewFakeTailedLogsOutputter()
		fakeExitHandler = &fake_exit_handler.FakeExitHandler{}
	})

	Describe("CreateAppCommand", func() {
		var createCommand cli.Command

		BeforeEach(func() {
			env := []string{"SHELL=/bin/bash", "COLOR=Blue"}
			appRunnerCommandFactoryConfig = command_factory.DockerRunnerCommandFactoryConfig{
				AppRunner:    appRunner,
				DockerRunner: dockerRunner,
				AppExaminer:  appExaminer,
				UI:           terminalUI,
				DockerMetadataFetcher: dockerMetadataFetcher,
				Domain:                domain,
				Env:                   env,
				Clock:                 clock,
				Logger:                logger,
				TailedLogsOutputter:   fakeTailedLogsOutputter,
				ExitHandler:           fakeExitHandler,
			}

			commandFactory := command_factory.NewDockerRunnerCommandFactory(appRunnerCommandFactoryConfig)
			createCommand = commandFactory.MakeCreateAppCommand()

			dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{}, nil)
		})

		It("creates a Docker based app as specified in the command via the AppRunner", func() {
			args := []string{
				"--cpu-weight=57",
				"--memory-mb=12",
				"--disk-mb=12",
				"--routes=3000:route-3000-yay,1111:route-1111-wahoo,1111:route-1111-me-too",
				"--working-dir=/applications",
				"--run-as-root=true",
				"--instances=22",
				"--env=TIMEZONE=CST",
				`--env=LANG="Chicago English"`,
				`--env=JAVA_OPTS="-Djava.arg=/dev/urandom"`,
				"--env=COLOR",
				"--env=UNSET",
				"--timeout=28s",
				"cool-web-app",
				"superfun/app:mycooltag",
				"--",
				"/start-me-please",
				"AppArg0",
				`--appFlavor="purple"`,
			}
			appExaminer.RunningAppInstancesInfoReturns(22, false, nil)

			test_helpers.ExecuteCommandWithArgs(createCommand, args)
			Expect(dockerMetadataFetcher.FetchMetadataCallCount()).To(Equal(1))
			Expect(dockerMetadataFetcher.FetchMetadataArgsForCall(0)).To(Equal("superfun/app:mycooltag"))

			Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
			createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)
			Expect(createDockerAppParameters.Name).To(Equal("cool-web-app"))
			Expect(createDockerAppParameters.StartCommand).To(Equal("/start-me-please"))
			Expect(createDockerAppParameters.RootFS).To(Equal("superfun/app:mycooltag"))
			Expect(createDockerAppParameters.AppArgs).To(Equal([]string{"AppArg0", "--appFlavor=\"purple\""}))
			Expect(createDockerAppParameters.Instances).To(Equal(22))
			Expect(createDockerAppParameters.EnvironmentVariables).To(Equal(map[string]string{
				"TIMEZONE":     "CST",
				"LANG":         `"Chicago English"`,
				"JAVA_OPTS":    `"-Djava.arg=/dev/urandom"`,
				"PROCESS_GUID": "cool-web-app",
				"COLOR":        "Blue",
				"UNSET":        "",
			}))
			Expect(createDockerAppParameters.Privileged).To(Equal(true))
			Expect(createDockerAppParameters.CPUWeight).To(Equal(uint(57)))
			Expect(createDockerAppParameters.MemoryMB).To(Equal(12))
			Expect(createDockerAppParameters.DiskMB).To(Equal(12))
			Expect(createDockerAppParameters.Monitor.Method).To(Equal(app_runner.PortMonitor))
			Expect(createDockerAppParameters.Timeout).To(Equal(time.Second * 28))
			Expect(createDockerAppParameters.RouteOverrides).To(ContainExactly(app_runner.RouteOverrides{
				app_runner.RouteOverride{HostnamePrefix: "route-3000-yay", Port: 3000},
				app_runner.RouteOverride{HostnamePrefix: "route-1111-wahoo", Port: 1111},
				app_runner.RouteOverride{HostnamePrefix: "route-1111-me-too", Port: 1111},
			}))
			Expect(createDockerAppParameters.NoRoutes).To(BeFalse())
			Expect(createDockerAppParameters.WorkingDir).To(Equal("/applications"))

			Expect(outputBuffer).To(test_helpers.Say("Creating App: cool-web-app\n"))
			Expect(outputBuffer).To(test_helpers.Say(colors.Green("cool-web-app is now running.\n")))
			Expect(outputBuffer).To(test_helpers.Say("App is reachable at:\n"))
			Expect(outputBuffer).To(test_helpers.Say(colors.Green("http://route-3000-yay.192.168.11.11.xip.io\n")))
			Expect(outputBuffer).To(test_helpers.Say(colors.Green("http://route-1111-wahoo.192.168.11.11.xip.io\n")))
			Expect(outputBuffer).To(test_helpers.Say(colors.Green("http://route-1111-me-too.192.168.11.11.xip.io\n")))
		})

		Context("when the PROCESS_GUID is passed in as --env", func() {
			It("sets the PROCESS_GUID to the value passed in", func() {
				args := []string{
					"app-to-start",
					"fun-org/app",
					"--env=PROCESS_GUID=MyHappyGuid",
				}
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{StartCommand: []string{""}}, nil)
				appExaminer.RunningAppInstancesInfoReturns(1, false, nil)

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
				createDockerAppParams := dockerRunner.CreateDockerAppArgsForCall(0)
				appEnvVars := createDockerAppParams.EnvironmentVariables
				processGuidEnvVar, found := appEnvVars["PROCESS_GUID"]

				Expect(found).To(BeTrue())
				Expect(processGuidEnvVar).To(Equal("MyHappyGuid"))
			})
		})

		Context("when a malformed routes flag is passed", func() {
			It("errors out when the port is not an int", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"--routes=woo:aahh",
					"--",
					"/start-me-please",
				}

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
				Expect(outputBuffer).To(test_helpers.Say(command_factory.MalformedRouteErrorMessage))
				Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.InvalidSyntax}))
			})

			It("errors out when there is no colon", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"--routes=8888",
					"--",
					"/start-me-please",
				}

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
				Expect(outputBuffer).To(test_helpers.Say(command_factory.MalformedRouteErrorMessage))
				Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.InvalidSyntax}))
			})
		})

		Describe("Exposed Ports", func() {
			BeforeEach(func() {
				appExaminer.RunningAppInstancesInfoReturns(1, false, nil)
			})

			It("exposes ports passed by --ports", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"--ports=8080,9090",
					"--",
					"/start-me-please",
				}

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
				createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)
				Expect(createDockerAppParameters.ExposedPorts).To(Equal([]uint16{8080, 9090}))
			})

			It("exposes ports from image metadata", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"--",
					"/start-me-please",
				}
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{
					ExposedPorts: []uint16{1200, 2701, 4302},
				}, nil)

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(outputBuffer).To(test_helpers.Say("No port specified, using exposed ports from the image metadata.\n\tExposed Ports: 1200, 2701, 4302\n"))
				createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)
				Expect(createDockerAppParameters.ExposedPorts).To(Equal([]uint16{1200, 2701, 4302}))
			})

			It("exposes --ports ports when both --ports and EXPOSE metadata exist", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"--ports=8080,9090",
					"--",
					"/start-me-please",
				}
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{
					ExposedPorts: []uint16{1200, 2701, 4302},
				}, nil)

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)
				Expect(createDockerAppParameters.ExposedPorts).To(Equal([]uint16{8080, 9090}))
			})

			Context("when the metadata does not have EXPOSE ports", func() {
				It("exposes the default port 8080", func() {
					args := []string{
						"cool-web-app",
						"superfun/app",
						"--no-monitor",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)
					Expect(createDockerAppParameters.ExposedPorts).To(Equal([]uint16{8080}))
				})
			})

			Context("when malformed --ports flag is passed", func() {
				It("blows up when you pass bad port strings", func() {
					args := []string{
						"--ports=1000,98feh34",
						"cool-web-app",
						"superfun/app:mycooltag",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
					Expect(outputBuffer).To(test_helpers.Say(command_factory.InvalidPortErrorMessage))
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.InvalidSyntax}))

				})

				It("errors out when any port is > 65535 (max Linux port number)", func() {
					args := []string{
						"cool-web-app",
						"superfun/app",
						"--ports=8080,65536",
						"--monitor-port=8080",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
					Expect(outputBuffer).To(test_helpers.Say(command_factory.InvalidPortErrorMessage))
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.InvalidSyntax}))

				})
			})
		})

		//TODO:  little wonky - this test makes sure we default stuff, but says it's dealing w/ fetcher
		Describe("interactions with the docker metadata fetcher", func() {
			Context("when the docker image is hosted on a docker registry", func() {
				It("creates a Docker based app with sensible defaults and checks for metadata to know the image exists", func() {
					args := []string{
						"cool-web-app",
						"awesome/app",
						"--",
						"/start-me-please",
					}
					appExaminer.RunningAppInstancesInfoReturns(1, false, nil)

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerMetadataFetcher.FetchMetadataCallCount()).To(Equal(1))
					Expect(dockerMetadataFetcher.FetchMetadataArgsForCall(0)).To(Equal("awesome/app"))

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
					createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)
					Expect(outputBuffer).To(test_helpers.Say("No port specified, image metadata did not contain exposed ports. Defaulting to 8080.\n"))
					Expect(createDockerAppParameters.Privileged).To(Equal(false))
					Expect(createDockerAppParameters.MemoryMB).To(Equal(128))
					Expect(createDockerAppParameters.DiskMB).To(Equal(0))
					Expect(createDockerAppParameters.Monitor.Port).To(Equal(uint16(8080)))
					Expect(createDockerAppParameters.ExposedPorts).To(Equal([]uint16{8080}))
					Expect(createDockerAppParameters.Instances).To(Equal(1))
					Expect(createDockerAppParameters.WorkingDir).To(Equal("/"))
				})
			})

			Context("when the docker metadata fetcher returns an error", func() {
				It("exposes the error from trying to fetch the Docker metadata", func() {
					args := []string{
						"cool-web-app",
						"superfun/app",
						"--",
						"/start-me-please",
					}
					dockerMetadataFetcher.FetchMetadataReturns(nil, errors.New("Docker Says No."))

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
					Expect(outputBuffer).To(test_helpers.Say("Error fetching image metadata: Docker Says No."))
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.BadDocker}))
				})
			})
		})

		Describe("Monitor Config", func() {

			BeforeEach(func() {
				appExaminer.RunningAppInstancesInfoReturns(1, false, nil)
			})

			Context("when --no-monitor is passed", func() {
				It("does not monitor", func() {
					args := []string{
						"cool-web-app",
						"superfun/app",
						"--no-monitor",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
					monitorConfig := dockerRunner.CreateDockerAppArgsForCall(0).Monitor
					Expect(monitorConfig.Method).To(Equal(app_runner.NoMonitor))
				})
			})

			Context("when --monitor-port is passed", func() {
				It("port-monitors a specified port", func() {
					args := []string{
						"--ports=1000,2000",
						"--monitor-port=2000",
						"cool-web-app",
						"superfun/app:mycooltag",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
					monitorConfig := dockerRunner.CreateDockerAppArgsForCall(0).Monitor
					Expect(monitorConfig.Method).To(Equal(app_runner.PortMonitor))
					Expect(monitorConfig.Port).To(Equal(uint16(2000)))
				})

				It("prints an error when the monitored port is not exposed", func() {
					args := []string{
						"--ports=1000,1200",
						"--monitor-port=2000",
						"cool-web-app",
						"superfun/app:mycooltag",
						"--",
						"/start-me-please",
					}
					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
					Expect(outputBuffer).To(test_helpers.Say(command_factory.MonitorPortNotExposed))
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.CommandFailed}))
				})
			})

			Context("when --monitor-url is passed", func() {
				It("url-monitors a specified url", func() {
					args := []string{
						"--ports=1000,2000",
						"--monitor-url=1000:/sup/yeah",
						"cool-web-app",
						"superfun/app",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
					monitorConfig := dockerRunner.CreateDockerAppArgsForCall(0).Monitor
					Expect(monitorConfig.Method).To(Equal(app_runner.URLMonitor))
					Expect(monitorConfig.Port).To(Equal(uint16(1000)))
				})

				It("prints an error if the url can't be split", func() {
					args := []string{
						"--ports=1000,2000",
						"--monitor-url=1000/sup/yeah",
						"cool-web-app",
						"superfun/app:mycooltag",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
					Expect(outputBuffer).To(test_helpers.Say(command_factory.InvalidPortErrorMessage))
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.InvalidSyntax}))
				})

				It("prints an error if the port is non-numeric", func() {
					args := []string{
						"--ports=1000,2000",
						"--monitor-url=TOTES:/sup/yeah",
						"cool-web-app",
						"superfun/app:mycooltag",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
					Expect(outputBuffer).To(test_helpers.Say(command_factory.InvalidPortErrorMessage))
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.InvalidSyntax}))
				})

				It("prints an error when the monitored url port is not exposed", func() {
					args := []string{
						"--ports=1000,2000",
						"--monitor-url=1200:/sup/yeah",
						"cool-web-app",
						"superfun/app:mycooltag",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
					Expect(outputBuffer).To(test_helpers.Say(command_factory.MonitorPortNotExposed))
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.CommandFailed}))
				})
			})

			Context("when no monitoring options are passed", func() {
				It("port-monitors the first exposed port", func() {
					args := []string{
						"--ports=1000,2000",
						"cool-web-app",
						"superfun/app:mycooltag",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
					monitorConfig := dockerRunner.CreateDockerAppArgsForCall(0).Monitor
					Expect(monitorConfig.Method).To(Equal(app_runner.PortMonitor))
					Expect(monitorConfig.Port).To(Equal(uint16(1000)))
				})

				It("sets a timeout", func() {
					args := []string{
						"--monitor-timeout=5s",
						"cool-web-app",
						"superfun/app",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
					monitorConfig := dockerRunner.CreateDockerAppArgsForCall(0).Monitor
					Expect(monitorConfig.Timeout).To(Equal(5 * time.Second))
				})
			})

			Context("when multiple monitoring options are passed", func() {
				It("no-monitor takes precedence", func() {
					args := []string{
						"--ports=1200",
						"--monitor-url=1200:/sup/yeah",
						"--no-monitor",
						"cool-web-app",
						"superfun/app",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
					monitorConfig := dockerRunner.CreateDockerAppArgsForCall(0).Monitor
					Expect(monitorConfig.Method).To(Equal(app_runner.NoMonitor))
				})

				It("monitor-url takes precedence over monitor-port", func() {
					args := []string{
						"--ports=1200",
						"--monitor-url=1200:/sup/yeah",
						"--monitor-port=1200",
						"cool-web-app",
						"superfun/app",
						"--",
						"/start-me-please",
					}

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
					monitorConfig := dockerRunner.CreateDockerAppArgsForCall(0).Monitor
					Expect(monitorConfig.Method).To(Equal(app_runner.URLMonitor))
					Expect(monitorConfig.Port).To(Equal(uint16(1200)))
				})
			})
		})

		Context("when the --no-routes flag is passed", func() {
			It("calls app runner with NoRoutes equal to true", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"--no-routes",
					"--",
					"/start-me-please",
				}
				appExaminer.RunningAppInstancesInfoReturns(1, false, nil)
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{}, nil)

				test_helpers.ExecuteCommandWithArgs(createCommand, args)
				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
				createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)

				Expect(createDockerAppParameters.NoRoutes).To(BeTrue())

				Expect(outputBuffer).NotTo(test_helpers.Say("App is reachable at:"))
				Expect(outputBuffer).NotTo(test_helpers.Say("http://cool-web-app.192.168.11.11.xip.io"))
			})
		})

		Context("when no working dir is provided, but the metadata has a working dir", func() {
			It("sets the working dir from the Docker metadata", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"--",
					"/start-me-please",
				}
				appExaminer.RunningAppInstancesInfoReturns(1, false, nil)
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{WorkingDir: "/work/it"}, nil)

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)
				Expect(createDockerAppParameters.WorkingDir).To(Equal("/work/it"))
			})
		})

		Context("when no start command is provided", func() {
			var args = []string{
				"cool-web-app",
				"fun-org/app",
			}

			BeforeEach(func() {
				appExaminer.RunningAppInstancesInfoReturns(1, false, nil)
			})

			It("creates a Docker app with the create command retrieved from the docker image metadata", func() {
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{WorkingDir: "/this/directory/right/here", StartCommand: []string{"/fetch-start", "arg1", "arg2"}}, nil)

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(dockerMetadataFetcher.FetchMetadataCallCount()).To(Equal(1))
				Expect(dockerMetadataFetcher.FetchMetadataArgsForCall(0)).To(Equal("fun-org/app"))

				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
				createDockerAppParameters := dockerRunner.CreateDockerAppArgsForCall(0)

				Expect(createDockerAppParameters.StartCommand).To(Equal("/fetch-start"))
				Expect(createDockerAppParameters.AppArgs).To(Equal([]string{"arg1", "arg2"}))
				Expect(createDockerAppParameters.RootFS).To(Equal("fun-org/app"))
				Expect(createDockerAppParameters.WorkingDir).To(Equal("/this/directory/right/here"))

				Expect(outputBuffer).To(test_helpers.Say("No working directory specified, using working directory from the image metadata...\n"))
				Expect(outputBuffer).To(test_helpers.Say("Working directory is:\n"))
				Expect(outputBuffer).To(test_helpers.Say("/this/directory/right/here\n"))

				Expect(outputBuffer).To(test_helpers.Say("No start command specified, using start command from the image metadata...\n"))
				Expect(outputBuffer).To(test_helpers.Say("Start command is:\n"))
				Expect(outputBuffer).To(test_helpers.Say("/fetch-start arg1 arg2\n"))
			})

			It("does not output the working directory if it is not set", func() {
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{StartCommand: []string{"/fetch-start"}}, nil)

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(outputBuffer).ToNot(test_helpers.Say("Working directory is:"))
				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
			})

			Context("when the metadata also has no start command", func() {
				It("outputs an error message and exits", func() {
					dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{}, nil)

					test_helpers.ExecuteCommandWithArgs(createCommand, args)

					Expect(outputBuffer).To(test_helpers.Say("Unable to determine start command from image metadata.\n"))
					Expect(dockerRunner.CreateDockerAppCallCount()).To(BeZero())
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.BadDocker}))

				})
			})
		})

		Context("when the timeout flag is not passed", func() {
			It("defaults the timeout to something reasonable", func() {
				args := []string{
					"app-to-timeout",
					"fun-org/app",
				}
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{StartCommand: []string{""}}, nil)
				appExaminer.RunningAppInstancesInfoReturns(1, false, nil)

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(1))
				createDockerAppParams := dockerRunner.CreateDockerAppArgsForCall(0)
				Expect(createDockerAppParams.Timeout).To(Equal(command_factory.DefaultPollingTimeout))
			})
		})

		Describe("polling for the app to start after desiring the app", func() {
			It("polls for the app to start with correct number of instances, outputting logs while the app starts", func() {
				args := []string{
					"--instances=10",
					"cool-web-app",
					"superfun/app",
					"--",
					"/start-me-please",
				}

				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{}, nil)
				appExaminer.RunningAppInstancesInfoReturns(0, false, nil)

				commandFinishChan := test_helpers.AsyncExecuteCommandWithArgs(createCommand, args)

				Eventually(outputBuffer).Should(test_helpers.Say("Creating App: cool-web-app"))

				Expect(fakeTailedLogsOutputter.OutputTailedLogsCallCount()).To(Equal(1))
				Expect(fakeTailedLogsOutputter.OutputTailedLogsArgsForCall(0)).To(Equal("cool-web-app"))

				Expect(appExaminer.RunningAppInstancesInfoCallCount()).To(Equal(1))
				Expect(appExaminer.RunningAppInstancesInfoArgsForCall(0)).To(Equal("cool-web-app"))

				clock.IncrementBySeconds(1)
				Expect(fakeTailedLogsOutputter.StopOutputtingCallCount()).To(Equal(0))

				appExaminer.RunningAppInstancesInfoReturns(9, false, nil)
				clock.IncrementBySeconds(1)
				Expect(commandFinishChan).ShouldNot(BeClosed())
				Expect(fakeTailedLogsOutputter.StopOutputtingCallCount()).To(Equal(0))

				appExaminer.RunningAppInstancesInfoReturns(10, false, nil)
				clock.IncrementBySeconds(1)

				Eventually(commandFinishChan).Should(BeClosed())
				Expect(fakeTailedLogsOutputter.StopOutputtingCallCount()).To(Equal(1))
				Expect(outputBuffer).To(test_helpers.SayNewLine())
				Expect(outputBuffer).To(test_helpers.Say(colors.Green("cool-web-app is now running.\n")))
				Expect(outputBuffer).To(test_helpers.Say("App is reachable at:\n"))
				Expect(outputBuffer).To(test_helpers.Say(colors.Green("http://cool-web-app.192.168.11.11.xip.io\n")))
			})

			Context("when the app does not start before the timeout elapses", func() {
				It("alerts the user the app took too long to start", func() {
					args := []string{
						"cool-web-app",
						"superfun/app",
						"--",
						"/start-me-please",
					}
					dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{}, nil)
					appExaminer.RunningAppInstancesInfoReturns(0, false, nil)

					commandFinishChan := test_helpers.AsyncExecuteCommandWithArgs(createCommand, args)

					Eventually(outputBuffer).Should(test_helpers.Say("Creating App: cool-web-app"))
					Expect(outputBuffer).To(test_helpers.SayNewLine())

					clock.IncrementBySeconds(120)

					Eventually(commandFinishChan).Should(BeClosed())

					Expect(outputBuffer).To(test_helpers.Say(colors.Red("Timed out waiting for the container to come up.")))
					Expect(outputBuffer).To(test_helpers.SayNewLine())
					Expect(outputBuffer).To(test_helpers.SayLine("This typically happens because docker layers can take time to download."))
					Expect(outputBuffer).To(test_helpers.SayLine("Lattice is still downloading your application in the background."))
					Expect(outputBuffer).To(test_helpers.SayLine("To view logs:\n\tltc logs cool-web-app"))
					Expect(outputBuffer).To(test_helpers.SayLine("To view status:\n\tltc status cool-web-app"))
					Expect(outputBuffer).To(test_helpers.Say("App will be reachable at:\n"))
					Expect(outputBuffer).To(test_helpers.Say(colors.Green("http://cool-web-app.192.168.11.11.xip.io\n")))
				})
			})

			Context("when there is a placement error when polling for the app to start", func() {
				It("prints an error message and exits", func() {
					args := []string{
						"--instances=10",
						"--ports=3000",
						"--working-dir=/applications",
						"cool-web-app",
						"superfun/app",
						"--",
						"/start-me-please",
					}

					dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{}, nil)
					appExaminer.RunningAppInstancesInfoReturns(0, false, nil)

					commandFinishChan := test_helpers.AsyncExecuteCommandWithArgs(createCommand, args)

					Eventually(outputBuffer).Should(test_helpers.Say("Monitoring the app on port 3000..."))
					Eventually(outputBuffer).Should(test_helpers.Say("Creating App: cool-web-app"))

					Expect(appExaminer.RunningAppInstancesInfoCallCount()).To(Equal(1))
					Expect(appExaminer.RunningAppInstancesInfoArgsForCall(0)).To(Equal("cool-web-app"))

					clock.IncrementBySeconds(1)
					Expect(fakeTailedLogsOutputter.StopOutputtingCallCount()).To(Equal(0))
					Expect(fakeExitHandler.ExitCalledWith).To(BeEmpty())

					appExaminer.RunningAppInstancesInfoReturns(9, true, nil)
					clock.IncrementBySeconds(1)
					Eventually(commandFinishChan).Should(BeClosed())
					Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.PlacementError}))
					Expect(fakeTailedLogsOutputter.StopOutputtingCallCount()).To(Equal(1))

					Expect(outputBuffer).To(test_helpers.SayNewLine())
					Expect(outputBuffer).To(test_helpers.Say(colors.Red("Error, could not place all instances: insufficient resources. Try requesting fewer instances or reducing the requested memory or disk capacity.")))
					Expect(outputBuffer).ToNot(test_helpers.Say("Timed out waiting for the container"))
				})
			})
		})

		Context("invalid syntax", func() {
			It("validates the CPU weight is in 1-100", func() {
				args := []string{
					"cool-app",
					"greatapp/greatapp",
					"--cpu-weight=0",
				}

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(outputBuffer).To(test_helpers.Say("Incorrect Usage: Invalid CPU Weight"))
				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
			})

			It("validates that the name and dockerImage are passed in", func() {
				args := []string{
					"justonearg",
				}

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(outputBuffer).To(test_helpers.Say("Incorrect Usage: APP_NAME and DOCKER_IMAGE are required"))
				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
			})

			It("validates that the terminator -- is passed in when a start command is specified", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"not-the-terminator",
					"start-me-up",
				}
				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(outputBuffer).To(test_helpers.Say("Incorrect Usage: '--' Required before start command"))
				Expect(dockerRunner.CreateDockerAppCallCount()).To(Equal(0))
			})
		})

		Context("when the app runner returns an error", func() {
			It("outputs error messages", func() {
				args := []string{
					"cool-web-app",
					"superfun/app",
					"--",
					"/start-me-please",
				}
				dockerMetadataFetcher.FetchMetadataReturns(&docker_metadata_fetcher.ImageMetadata{}, nil)
				dockerRunner.CreateDockerAppReturns(errors.New("Major Fault"))

				test_helpers.ExecuteCommandWithArgs(createCommand, args)

				Expect(outputBuffer).To(test_helpers.Say("Error creating app: Major Fault"))
				Expect(fakeExitHandler.ExitCalledWith).To(Equal([]int{exit_codes.CommandFailed}))
			})
		})
	})

})