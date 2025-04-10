package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/web"
	"example.poc/device-monitoring-system/internal/worker"
	"example.poc/device-monitoring-system/pkg"
	"github.com/rs/zerolog/log"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <command>\n", os.Args[0])
		fmt.Println("Commands:")
		fmt.Println("  web_service              Start the web service")
		fmt.Println("  polling_worker   		Start the polling worker")
		fmt.Println("  start_device_simulator   Start one device simulator")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "web_service":
		startWebService()
	case "polling_worker":
		startPollingWorker()
	case "start_device_simulator":
		startDeviceSimulator()
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Printf("Usage: %s <command>\n", os.Args[0])
		fmt.Println("Commands:")
		fmt.Println("  web_service              Start the web service")
		fmt.Println("  polling_worker   		Start the polling worker")
		fmt.Println("  start_device_simulator   Start one device simulator")
		os.Exit(1)
	}
}

func startWebService() {
	router, err := web.NewRouter()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create router")
	}
	if err = http.ListenAndServe(fmt.Sprintf(":%d", config.WebServicePort()), router); err != nil {
		log.Fatal().Err(err).Msg("web server stopped")
	}
}

func startPollingWorker() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	pollingWorker, err := worker.NewPollingWorker(nil)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create polling worker")
	}

	go func() {
		err := pollingWorker.Start(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal().Err(err).Msg("failed to start device polling worker")
		}
		cancel()
	}()

	<-ctx.Done()

	log.Info().Msg("shutting down device polling worker in 10 seconds...")
	time.Sleep(10 * time.Second)
	log.Info().Msg("worker shutdown")
}

func startDeviceSimulator() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	ds := pkg.NewDeviceSimulator()
	if err := ds.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to start device simulator")
	}
}
