package agentLog

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	LevelDebug   = 0
	LevelInfo    = 1
	LevelWarning = 2
	LevelError   = 3
)

var (
	level       = LevelDebug
	logFile     *os.File
	AgentLogger *zap.SugaredLogger
)

func InitLogger() {

}

func getEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder // 修改时间编码器

	// 在日志文件中使用大写字母记录日志级别
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	// NewConsoleEncoder 打印更符合人们观察的方式
	return zapcore.NewConsoleEncoder(encoderConfig)
}

func getLogWriter(logName string) zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   logName, //日志文件的位置
		MaxSize:    100,     //在进行切割之前，日志文件的最大大小（以MB为单位）；
		MaxBackups: 10,      //保留旧文件的最大个数；
		MaxAge:     365,     //保留旧文件的最大天数；
		Compress:   false,   //是否压缩归档旧文件
	}
	return zapcore.AddSync(lumberJackLogger)
}

func Init(logName string) {
	encoder := getEncoder()
	writeSyncer := getLogWriter(logName)
	core := zapcore.NewCore(encoder, writeSyncer, zapcore.DebugLevel)

	// zap.AddCaller()  添加将调用函数信息记录到日志中的功能。
	logger := zap.New(core, zap.AddCaller())
	AgentLogger = logger.Sugar()
}

func Level() int {
	return level
}

func SetLevel(l int) error {
	if l >= LevelDebug || l <= LevelError {
		level = l
		return nil
	} else {
		return errors.New("SetLevel: level 0~3")
	}
}

func Debug(v ...interface{}) {
	if level <= LevelDebug {
		//AgentLogger.Printf("Debug: %v\n", v)
		AgentLogger.Debug(v)
	}
}

func Info(v ...interface{}) {
	if level <= LevelInfo {
		//AgentLogger.Printf("Info: %v\n", v)
		AgentLogger.Info(v)
	}
}

func Warning(v ...interface{}) {
	if level <= LevelWarning {
		//AgentLogger.Printf("Warning: %v\n", v)
		AgentLogger.Warn(v)
	}
}

func Error(v ...interface{}) {
	if level <= LevelError {
		//AgentLogger.Printf("Error: %v\n", v)
		AgentLogger.Error(v)
	}
}

func HttpLog(level string, r *http.Request, msg string) {
	switch level {
	case "debug":
		AgentLogger.Debug(fmt.Sprintf("%s\t%s\t%s\t%s", r.Method, r.RequestURI, r.RemoteAddr, msg))
	case "info":
		AgentLogger.Info(fmt.Sprintf("%s\t%s\t%s\t%s", r.Method, r.RequestURI, r.RemoteAddr, msg))
	case "warning":
		AgentLogger.Warn(fmt.Sprintf("%s\t%s\t%s\t%s", r.Method, r.RequestURI, r.RemoteAddr, msg))
	case "error":
		AgentLogger.Error(fmt.Sprintf("%s\t%s\t%s\t%s", r.Method, r.RequestURI, r.RemoteAddr, msg))
	}
}
