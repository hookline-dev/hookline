package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)



func main() {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	appMode := os.Getenv("APP_MODE")
	if appMode == "" {
		appMode = "all"
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" { 
		logLevel = "debug"
	}

	logFormat := os.Getenv("LOG_FORMAT")
	if logFormat == "" { 
		logFormat = "text"
	}

	mode := flag.String("mode", appMode, "режим запуска: api | worker | all")
	flag.Parse()
	
	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
    	level = slog.LevelWarn
	case "error":
    	level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	
	var handler slog.Handler
	if logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
		
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	slog.Info("запуск", "mode", *mode)
		
	srv := &http.Server{
		Addr: ":" + port,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func(){
		slog.Info("сервер запущен", "addr", ":"+port)
		err := srv.ListenAndServe()
		if err != nil && !errors.Is (err, http.ErrServerClosed){
			slog.Error("ошибка сервера", "err", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
    	slog.Error("ошибка при остановке", "err", err)
	}
	slog.Info("сервер остановлен")
}
