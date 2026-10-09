package engine

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/natefinch/lumberjack"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// This file will be created within the fetchit pod
const logFile = "/opt/mount/fetchit.log"

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start fetchit engine",
	Long:  `Start fetchit engine`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
		defer stop()
		return fetchitConfig.run(ctx)
	},
}

var logger *zap.SugaredLogger

func init() {
	fetchitConfig = newFetchitConfig()
	fetchitCmd.AddCommand(startCmd)
}

func InitLogger() {
	syncer := zap.CombineWriteSyncers(os.Stdout, getLogWriter())
	encoder := getEncoder()
	level := zap.InfoLevel
	if os.Getenv("FETCHIT_DEBUG") != "" {
		level = zap.DebugLevel
	}
	core := zapcore.NewCore(encoder, syncer, zap.NewAtomicLevelAt(level))
	l := zap.New(core, zap.AddCaller())
	logger = l.Sugar()
	logger.Debug("Fetchit debug logging enabled.")
}

func getEncoder() zapcore.Encoder {
	cfg := zap.NewProductionEncoderConfig()
	// The format time can be customized
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncodeLevel = zapcore.CapitalLevelEncoder
	return zapcore.NewConsoleEncoder(cfg)
}

// Save file log cut
func getLogWriter() zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    1,     // File content size, MB
		MaxBackups: 5,     // Maximum number of old files retained
		MaxAge:     30,    // Maximum number of days to keep old files
		Compress:   false, // Is the file compressed
	}
	return zapcore.AddSync(lumberJackLogger)
}
