package main

// metrixd — минимальный Prometheus-экспортер.
// Экспортирует стандартные метрики процесса/Go и две свои метрики:
//   1) host_environment_info{type="vm|container|physical"} 1 — тип окружения;
//   2) metrixd_build_info{version,go_version} 1 — информация о сборке.
//
// Принципы: простота (минимум зависимостей), корректность (надёжное
// определение окружения через systemd-detect-virt с резервными эвристиками),
// безопасность (слушаем 127.0.0.1 по умолчанию), читабельность.

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Флаг адреса прослушивания. По умолчанию слушаем только loopback.
var listenAddr = flag.String("listen-address", "127.0.0.1:8080", "адрес для HTTP-сервера (host:port)")

// Версия бинарника (передаётся через -ldflags "-X main.version=...").
var version = "dev"

func main() {
	flag.Parse()

	// Регистр метрик: только то, что нам нужно (без глобальной регистрации).
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	// 1) Метрика "тип окружения": one-hot через Gauge с константной меткой.
	hostType := detectHostType()
	hostEnvGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "host_environment_info",
		Help:        "Тип окружения хоста (one-hot): type={vm|container|physical}",
		ConstLabels: prometheus.Labels{"type": hostType},
	})
	hostEnvGauge.Set(1)
	reg.MustRegister(hostEnvGauge)

	// 2) Метрика "информация о сборке": часто используемый паттерн в экспортерах.
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "metrixd_build_info",
		Help: "Информация о сборке экспортерa",
		ConstLabels: prometheus.Labels{
			"version":    version,
			"go_version": runtime.Version(),
		},
	})
	buildInfo.Set(1)
	reg.MustRegister(buildInfo)

	// HTTP-маршруты: метрики доступны на "/" и на "/metrics".
	mux := http.NewServeMux()
	metricsHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	mux.Handle("/", metricsHandler)
	mux.Handle("/metrics", metricsHandler)

	// Аккуратные таймауты HTTP-сервера.
	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	log.Printf("metrixd %s запущен; listen=%s; метрики на / и /metrics; тип окружения: %s",
		version, *listenAddr, hostType)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("ошибка HTTP-сервера: %v", err)
	}
}

// detectHostType — определяет тип окружения: контейнер, ВМ или физический хост.
// Стратегия: сперва systemd-detect-virt (надежный источник), затем лёгкие
// эвристики по файлам/cgroup. При неудаче — "physical".
func detectHostType() string {
	// Контейнер?
	if runOK(2*time.Second, "systemd-detect-virt", "--container") {
		return "container"
	}
	// Виртуальная машина?
	if runOK(2*time.Second, "systemd-detect-virt", "--vm") {
		return "vm"
	}
	// Резервные эвристики для контейнеров:
	if fileExists("/.dockerenv") {
		return "container"
	}
	if b, err := os.ReadFile("/run/systemd/container"); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return "container"
	}
	if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(b)
		if strings.Contains(s, "docker") ||
			strings.Contains(s, "kubepods") ||
			strings.Contains(s, "containerd") ||
			strings.Contains(s, "libpod") ||
			strings.Contains(s, "podman") ||
			strings.Contains(s, "lxc") {
			return "container"
		}
	}
	return "physical"
}

// runOK — запустить команду с таймаутом и вернуть true при коде выхода 0.
func runOK(timeout time.Duration, name string, args ...string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// fileExists — существует ли путь и это не каталог.
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// init — печать краткой справки по флагам (удобно при запуске вручную).
func init() {
	fmt.Fprintf(os.Stderr, "metrixd %s — Prometheus-экспортер; флаги: -listen-address\n", version)
}
