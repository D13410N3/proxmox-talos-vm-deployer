package main

import (
	"encoding/json"
	"fmt"
)

type Logger struct {
	Level int
}

const (
	LevelDebug = iota
	LevelInfo
	LevelError
)

func (l *Logger) Info(format string, a ...interface{}) {
	l.log(LevelInfo, "NORMAL", format, a...)
}

func (l *Logger) Debug(format string, a ...interface{}) {
	l.log(LevelDebug, "DEBUG", format, a...)
}

func (l *Logger) Error(format string, a ...interface{}) {
	l.log(LevelError, "ERROR", format, a...)
}

func (l *Logger) log(messageLevel int, severity string, format string, a ...interface{}) {
	if l.Level > messageLevel {
		return
	}
	message := fmt.Sprintf(format, a...)
	logEntry := map[string]string{
		"severity": severity,
		"message":  message,
	}
	jsonData, err := json.Marshal(logEntry)
	if err != nil {
		fmt.Println(`{"severity":"ERROR","message":"Failed to marshal log entry"}`)
		return
	}
	fmt.Println(string(jsonData))
}
