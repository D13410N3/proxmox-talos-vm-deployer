package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/caarlos0/env/v6"
	"github.com/getsentry/sentry-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v2"
)

var (
	config                    Config
	authToken                 string
	proxmoxBaseAddr           string
	proxmoxToken              string
	logger                    *Logger
	talosMachineTemplate      string
	talosControlPlaneEndpoint string
	mikrotikIP                string
	mikrotikPort              string
	mikrotikUsername          string
	mikrotikPassword          string
)

func main() {
	rand.Seed(time.Now().UnixNano())

	var appConfig AppConfig
	if err := env.Parse(&appConfig); err != nil {
		log.Fatalf("Failed to parse environment variables: %s", err)
	}

	proxmoxBaseAddr = appConfig.ProxmoxBaseAddr
	proxmoxToken = appConfig.ProxmoxToken
	authToken = appConfig.AuthToken
	talosMachineTemplate = appConfig.TalosMachineTemplate
	talosControlPlaneEndpoint = appConfig.TalosControlPlaneEndpoint
	mikrotikIP = appConfig.MikrotikIP
	mikrotikPort = appConfig.MikrotikPort
	mikrotikUsername = appConfig.MikrotikUsername
	mikrotikPassword = appConfig.MikrotikPassword

	// Initialize HTTP client with SSL verification setting
	httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: !appConfig.VerifySSL,
			},
		},
	}

	logger = &Logger{Level: appConfig.LogLevel}

	if err := sentry.Init(sentry.ClientOptions{Dsn: appConfig.SentryDSN}); err != nil {
		logger.Error("sentry.Init: %s", err)
		os.Exit(1)
	}
	defer sentry.Flush(2 * time.Second)

	data, err := ioutil.ReadFile(appConfig.ConfigPath)
	if err != nil {
		logger.Error("Failed to read config file: %s", err)
		os.Exit(1)
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		logger.Error("Failed to parse config: %s", err)
		os.Exit(1)
	}

	initMetrics()

	http.HandleFunc("/health-check", healthCheckHandler)
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/api/v1/create", createVMHandler)
	http.HandleFunc("/api/v1/delete", deleteVMHandler)

	serverAddr := fmt.Sprintf("%s:%s", appConfig.ListenAddr, appConfig.ListenPort)
	logger.Info("Server starting on %s", serverAddr)
	if err := http.ListenAndServe(serverAddr, nil); err != nil {
		logger.Error("Server error: %s", err)
		os.Exit(1)
	}
}
