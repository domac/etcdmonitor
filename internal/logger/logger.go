package logger

import (
	"os"
	"path/filepath"

	"etcdmonitor/internal/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/lumberjack.v2"
)

// global sugar logger
var sugar *zap.SugaredLogger
var zapLogger *zap.Logger

// Init 根据配置初始化全局 zap logger
// 底层使用 lumberjack 实现日志滚动切割
func Init(cfg *config.Config) error {
	// 解析日志级别
	level, err := zapcore.ParseLevel(cfg.Log.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}

	// 确保日志目录存在
	if err := os.MkdirAll(cfg.Log.Dir, 0755); err != nil {
		return err
	}

	// lumberjack 滚动写入器
	logPath := filepath.Join(cfg.Log.Dir, cfg.Log.Filename)
	lumberWriter := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    cfg.Log.MaxSizeMB, // MB
		MaxBackups: cfg.Log.MaxFiles,
		MaxAge:     cfg.Log.MaxAge, // days
		Compress:   cfg.Log.Compress,
		LocalTime:  true,
	}

	// 编码器配置 - Console 格式，人类可读
	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,    // INFO, WARN, ERROR
		EncodeTime:     zapcore.ISO8601TimeEncoder,     // 2024-01-01T10:00:00.000+0800
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	encoder := zapcore.NewConsoleEncoder(encoderCfg)

	// 构建 core：文件输出 + 可选控制台输出
	fileSyncer := zapcore.AddSync(lumberWriter)
	var core zapcore.Core

	if cfg.Log.Console {
		consoleSyncer := zapcore.Lock(os.Stdout)
		core = zapcore.NewTee(
			zapcore.NewCore(encoder, fileSyncer, level),
			zapcore.NewCore(encoder, consoleSyncer, level),
		)
	} else {
		core = zapcore.NewCore(encoder, fileSyncer, level)
	}

	// 创建 logger，附加 caller 信息
	zapLogger = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	sugar = zapLogger.Sugar()

	return nil
}

// Sync 刷新日志缓冲（程序退出前调用）
func Sync() {
	if sugar != nil {
		_ = sugar.Sync()
	}
}

// L 返回底层 *zap.Logger（供需要结构化日志的模块使用）
func L() *zap.Logger {
	if zapLogger != nil {
		return zapLogger
	}
	// fallback: 返回一个 nop logger，避免空指针
	return zap.NewNop()
}

// IsInitialized 返回 logger 是否已初始化
func IsInitialized() bool {
	return sugar != nil
}

// === Sugar 风格全局函数 ===

func Debug(args ...interface{})                   { if sugar != nil { sugar.Debug(args...) } }
func Debugf(template string, args ...interface{}) { if sugar != nil { sugar.Debugf(template, args...) } }

func Info(args ...interface{})                    { if sugar != nil { sugar.Info(args...) } }
func Infof(template string, args ...interface{})  { if sugar != nil { sugar.Infof(template, args...) } }

func Warn(args ...interface{})                    { if sugar != nil { sugar.Warn(args...) } }
func Warnf(template string, args ...interface{})  { if sugar != nil { sugar.Warnf(template, args...) } }

func Error(args ...interface{})                   { if sugar != nil { sugar.Error(args...) } }
func Errorf(template string, args ...interface{}) { if sugar != nil { sugar.Errorf(template, args...) } }

func Fatal(args ...interface{})                   { if sugar != nil { sugar.Fatal(args...) } }
func Fatalf(template string, args ...interface{}) { if sugar != nil { sugar.Fatalf(template, args...) } }
