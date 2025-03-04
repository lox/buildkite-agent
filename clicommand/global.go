package clicommand

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/version"
	"github.com/oleiade/reflections"
	"github.com/urfave/cli"
)

const (
	DefaultEndpoint = "https://agent.buildkite.com/v3"
)

var AgentAccessTokenFlag = cli.StringFlag{
	Name:   "agent-access-token",
	Value:  "",
	Usage:  "The access token used to identify the agent",
	EnvVar: "BUILDKITE_AGENT_ACCESS_TOKEN",
}

var AgentRegisterTokenFlag = cli.StringFlag{
	Name:   "token",
	Value:  "",
	Usage:  "Your account agent token",
	EnvVar: "BUILDKITE_AGENT_TOKEN",
}

var EndpointFlag = cli.StringFlag{
	Name:   "endpoint",
	Value:  DefaultEndpoint,
	Usage:  "The Agent API endpoint",
	EnvVar: "BUILDKITE_AGENT_ENDPOINT",
}

var NoHTTP2Flag = cli.BoolFlag{
	Name:   "no-http2",
	Usage:  "Disable HTTP2 when communicating with the Agent API.",
	EnvVar: "BUILDKITE_NO_HTTP2",
}

var DebugFlag = cli.BoolFlag{
	Name:   "debug",
	Usage:  "Enable debug mode. Synonym for ′--log-level debug′. Takes precedence over ′--log-level′",
	EnvVar: "BUILDKITE_AGENT_DEBUG",
}

var LogLevelFlag = cli.StringFlag{
	Name:   "log-level",
	Value:  "notice",
	Usage:  "Set the log level for the agent, making logging more or less verbose. Defaults to notice. Allowed values are: debug, info, error, warn, fatal",
	EnvVar: "BUILDKITE_AGENT_LOG_LEVEL",
}

var ProfileFlag = cli.StringFlag{
	Name:   "profile",
	Usage:  "Enable a profiling mode, either cpu, memory, mutex or block",
	EnvVar: "BUILDKITE_AGENT_PROFILE",
}

var DebugHTTPFlag = cli.BoolFlag{
	Name:   "debug-http",
	Usage:  "Enable HTTP debug mode, which dumps all request and response bodies to the log",
	EnvVar: "BUILDKITE_AGENT_DEBUG_HTTP",
}

var NoColorFlag = cli.BoolFlag{
	Name:   "no-color",
	Usage:  "Don't show colors in logging",
	EnvVar: "BUILDKITE_AGENT_NO_COLOR",
}

var ExperimentsFlag = cli.StringSliceFlag{
	Name:   "experiment",
	Value:  &cli.StringSlice{},
	Usage:  "Enable experimental features within the buildkite-agent",
	EnvVar: "BUILDKITE_AGENT_EXPERIMENT",
}

var RedactedVars = cli.StringSliceFlag{
	Name:   "redacted-vars",
	Usage:  "Pattern of environment variable names containing sensitive values",
	EnvVar: "BUILDKITE_REDACTED_VARS",
	Value:  &cli.StringSlice{"*_PASSWORD", "*_SECRET", "*_TOKEN", "*_ACCESS_KEY", "*_SECRET_KEY"},
}

func CreateLogger(cfg any) logger.Logger {
	var l logger.Logger
	logFormat := "text"

	// Check the LogFormat config field
	if logFormatCfg, err := reflections.GetField(cfg, "LogFormat"); err == nil {
		if logFormatString, ok := logFormatCfg.(string); ok {
			logFormat = logFormatString
		}
	}

	// Create a logger based on the type
	switch logFormat {
	case "text", "":
		printer := logger.NewTextPrinter(os.Stderr)

		// Show agent fields as a prefix
		printer.IsPrefixFn = func(field logger.Field) bool {
			switch field.Key() {
			case "agent", "hook":
				return true
			default:
				return false
			}
		}

		// Turn off color if a NoColor option is present
		noColor, err := reflections.GetField(cfg, "NoColor")
		if noColor == true && err == nil {
			printer.Colors = false
		} else {
			printer.Colors = true
		}

		l = logger.NewConsoleLogger(printer, os.Exit)
	case "json":
		l = logger.NewConsoleLogger(logger.NewJSONPrinter(os.Stdout), os.Exit)
	default:
		fmt.Printf("Unknown log-format of %q, try text or json\n", logFormat)
		os.Exit(1)
	}

	l.SetLevel(logger.NOTICE)

	err := handleLogLevelFlag(l, cfg)
	if err != nil {
		l.Warn("Error when setting log level: %v. Defaulting log level to NOTICE", err)
	}

	// Enable debugging if a Debug option is present
	debugI, _ := reflections.GetField(cfg, "Debug")
	if debug, ok := debugI.(bool); ok && debug {
		l.SetLevel(logger.DEBUG)
	}

	return l
}

func HandleProfileFlag(l logger.Logger, cfg any) func() {
	// Enable profiling a profiling mode if Profile is present
	modeField, _ := reflections.GetField(cfg, "Profile")
	if mode, ok := modeField.(string); ok && mode != "" {
		return Profile(l, mode)
	}
	return func() {}
}

func HandleGlobalFlags(l logger.Logger, cfg any) func() {
	// Enable experiments
	experimentNames, err := reflections.GetField(cfg, "Experiments")
	if err == nil {
		experimentNamesSlice, ok := experimentNames.([]string)
		if ok {
			for _, name := range experimentNamesSlice {
				known := experiments.Enable(name)
				if !known {
					l.Warn("Unknown experiment enabled: %q", name)
					continue
				}
				l.Debug("Enabled experiment %q", name)
			}
		}
	}

	// Handle profiling flag
	return HandleProfileFlag(l, cfg)
}

func handleLogLevelFlag(l logger.Logger, cfg any) error {
	logLevel, err := reflections.GetField(cfg, "LogLevel")
	if err != nil {
		return err
	}

	llStr, ok := logLevel.(string)
	if !ok {
		return fmt.Errorf("log level %v (%T) couldn't be cast to string", logLevel, logLevel)
	}

	level, err := logger.LevelFromString(llStr)
	if err != nil {
		return err
	}

	l.SetLevel(level)
	return nil
}

func UnsetConfigFromEnvironment(c *cli.Context) error {
	flags := append(c.App.Flags, c.Command.Flags...)
	for _, fl := range flags {
		// use golang reflection to find EnvVar values on flags
		r := reflect.ValueOf(fl)
		f := reflect.Indirect(r).FieldByName("EnvVar")
		if !f.IsValid() {
			return errors.New("EnvVar field not found on flag")
		}
		// split comma delimited env
		if envVars := f.String(); envVars != "" {
			for _, env := range strings.Split(envVars, ",") {
				os.Unsetenv(env)
			}
		}
	}
	return nil
}

func loadAPIClientConfig(cfg any, tokenField string) api.Config {
	conf := api.Config{
		UserAgent: version.UserAgent(),
	}

	// Enable HTTP debugging
	debugHTTP, err := reflections.GetField(cfg, "DebugHTTP")
	if debugHTTP == true && err == nil {
		conf.DebugHTTP = true
	}

	endpoint, err := reflections.GetField(cfg, "Endpoint")
	if endpoint != "" && err == nil {
		conf.Endpoint = endpoint.(string)
	}

	token, err := reflections.GetField(cfg, tokenField)
	if token != "" && err == nil {
		conf.Token = token.(string)
	}

	noHTTP2, err := reflections.GetField(cfg, "NoHTTP2")
	if err == nil {
		conf.DisableHTTP2 = noHTTP2.(bool)
	}

	return conf
}
