// Команда transformer — локальный прокси-преобразователь входящих
// LLM-запросов: читает правила из файла, применяет их к телу запроса
// POST /v1/chat/completions и пересылает запрос на апстрим.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kartashoffvbif-blip/transmutator/internal/config"
	"github.com/kartashoffvbif-blip/transmutator/internal/httpapi"
	"github.com/kartashoffvbif-blip/transmutator/internal/logging"
	"github.com/kartashoffvbif-blip/transmutator/internal/proxy"
	"github.com/kartashoffvbif-blip/transmutator/internal/storage"
	"github.com/kartashoffvbif-blip/transmutator/internal/transform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "config.yaml", "путь к конфигурационному файлу")
	flag.Parse()

	// 1. конфигурация
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("конфигурация: %w", err)
	}
	logger := logging.New(cfg.Debug)

	// 2. правила: загрузка и полная проверка при старте
	ruleList, err := storage.NewFileStore(cfg.RulesFile).Load()
	if err != nil {
		return fmt.Errorf("правила: %w", err)
	}
	engine, err := transform.New(ruleList, cfg.Timeout)
	if err != nil {
		return fmt.Errorf("правила: %w", err)
	}

	// 3. апстрим и обработчики
	upstream, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return fmt.Errorf("upstream_url: %w", err)
	}
	handler := &httpapi.Handler{
		Proxy:  proxy.New(upstream),
		Engine: engine,
		Logger: logger,
		Debug:  cfg.Debug,
	}

	// 4. HTTP-сервер с аккуратным завершением по SIGINT/SIGTERM
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// WriteTimeout намеренно не задан: ответы апстрима стримятся
	}
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	logger.Info("сервис запущен",
		"addr", cfg.ListenAddr,
		"upstream", cfg.UpstreamURL,
		"rules", len(ruleList),
		"debug", cfg.Debug,
	)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return fmt.Errorf("http-сервер: %w", err)
	case sig := <-stop:
		logger.Info("получен сигнал, останавливаюсь", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		logger.Info("сервис остановлен")
		return nil
	}
}
