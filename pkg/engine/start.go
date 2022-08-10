package engine

import (
	"github.com/natefinch/lumberjack"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
)

// This file will be created within the fetchit pod
const logFile = "/opt/mount/fetchit.log"

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start fetchit engine",
	Long:  `Start fetchit engine`,
	Run: func(cmd *cobra.Command, args []string) {
		fetchit = fetchitConfig.InitConfig(true)
		fetchit.RunTargets()
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
	core := zapcore.NewCore(encoder, syncer, zap.NewAtomicLevelAt(zap.InfoLevel))
	l := zap.New(core)
	logger = l.Sugar()
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
