package main

import (
	steno "github.com/cloudfoundry/gosteno"
	"github.com/dinp/gorouter/access_log"
	vcap "github.com/dinp/gorouter/common"
	"github.com/dinp/gorouter/config"
	"github.com/dinp/gorouter/proxy"
	rregistry "github.com/dinp/gorouter/registry"
	"github.com/dinp/gorouter/router"
	rvarz "github.com/dinp/gorouter/varz"
	"flag"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
	"fmt"
)

var configFile string

func init() {
	flag.StringVar(&configFile, "c", "", "Configuration File")

	flag.Parse()
	fmt.Println("configFile is ",configFile)
}

func main() {
	fmt.Println("begin Main")
	fmt.Println("1.begin read config")
	c := config.DefaultConfig()
	logCounter := vcap.NewLogCounter()
	InitLoggerFromConfig(c, logCounter)

	if configFile != "" {
		c = config.InitConfigFromFile(configFile)
	}
	fmt.Println("1.end read config")
	//logger.Info("end read config file")
	// setup number of procs
	if c.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(c.GoMaxProcs)
	}
	
	
	InitLoggerFromConfig(c, logCounter)
	fmt.Println("begin steno.NewLogger")
	logger := steno.NewLogger("router.log")
	fmt.Println("end steno.NewLogger")
	logger.Debug("test logger")
	//logger.Info("begin read config file")
	//dropsonde.Initialize(c.Logging.MetronAddress, c.Logging.JobName)
	fmt.Println("main.go====has read config")
	registry := rregistry.NewRouteRegistry(c)

	varz := rvarz.NewVarz(registry)

	accessLogger, err := access_log.CreateRunningAccessLogger(c)
	if err != nil {
		logger.Fatalf("Error creating access logger: %s\n", err)
	}

	args := proxy.ProxyArgs{
		EndpointTimeout: c.EndpointTimeout,
		Ip:              c.Ip,
		TraceKey:        c.TraceKey,
		Registry:        registry,
		Reporter:        varz,
		AccessLogger:    accessLogger,
	}
	p := proxy.NewProxy(args)
	fmt.Println("main.go====has exec proxy.NewProxy(args)")
	rregistry.InitRedisConnPool(c)
	defer rregistry.RedisConnPool.Close()

	router, err := router.NewRouter(c, p, registry, varz, logCounter)
	if err != nil {
		logger.Errorf("An error occurred: %s", err.Error())
		os.Exit(1)
	}
	fmt.Println("main.go====has rregistry.InitRedisConnPool(c)")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)

	errChan := router.Run()

	logger.Info("gorouter.started")
	fmt.Println("gorouter.started")
	select {
	case err := <-errChan:
		if err != nil {
			logger.Errorf("Error occurred: %s", err.Error())
			os.Exit(1)
		}
	case sig := <-signals:
		go func() {
			for sig := range signals {
				logger.Infod(
					map[string]interface{}{
						"signal": sig.String(),
					},
					"gorouter.signal.ignored",
				)
			}
		}()

		if sig == syscall.SIGUSR1 {
			logger.Infod(
				map[string]interface{}{
					"timeout": (c.DrainTimeout).String(),
				},
				"gorouter.draining",
			)

			router.Drain(c.DrainTimeout)
		}

		stoppingAt := time.Now()

		logger.Info("gorouter.stopping")

		router.Stop()

		logger.Infod(
			map[string]interface{}{
				"took": time.Since(stoppingAt).String(),
			},
			"gorouter.stopped",
		)
	}

	os.Exit(0)
}

func InitLoggerFromConfig(c *config.Config, logCounter *vcap.LogCounter) {
	fmt.Println("begin InitLoggerFromConfig ")
	l, err := steno.GetLogLevel(c.Logging.Level)
	if err != nil {
		panic(err)
	}

	s := make([]steno.Sink, 0, 3)
	if c.Logging.File != "" {
		s = append(s, steno.NewFileSink(c.Logging.File))
	} else {
		s = append(s, steno.NewIOSink(os.Stdout))
	}

	if c.Logging.Syslog != "" {
		s = append(s, steno.NewSyslogSink(c.Logging.Syslog))
	}

	s = append(s, logCounter)

	stenoConfig := &steno.Config{
		Sinks: s,
		Codec: steno.NewJsonCodec(),
		Level: l,
	}

	steno.Init(stenoConfig)
	fmt.Println("end InitLoggerFromConfig")
}
