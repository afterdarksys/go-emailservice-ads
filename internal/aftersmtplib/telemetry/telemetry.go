package telemetry

import (
	"fmt"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// Log is the global heavily-optimized structured logger.
	Log *zap.Logger
)

// Init sets up the global zap logger and starts the Prometheus metrics server.
func Init(metricsAddr string, isDev bool) error {
	var err error

	// 1. Initialize Zap Logger
	if isDev {
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		Log, err = config.Build(zap.AddCaller())
	} else {
		// Production JSON logging for ELK/Datadog
		config := zap.NewProductionConfig()
		Log, err = config.Build(zap.AddCaller())
	}

	if err != nil {
		return fmt.Errorf("failed to initialize zap logger: %w", err)
	}

	// 2. Start Prometheus Metrics Server
	if metricsAddr != "" {
		go func() {
			Log.Info("Starting Prometheus metrics server", zap.String("address", metricsAddr))
			http.Handle("/metrics", promhttp.Handler())
			if err := http.ListenAndServe(metricsAddr, nil); err != nil {
				Log.Error("Prometheus server failed", zap.Error(err))
				os.Exit(1)
			}
		}()
	}

	return nil
}

// Sync flushes any buffered log entries. Should be called before process exit.
func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}
