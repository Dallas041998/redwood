package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// support for reloading configuration without restarting Redwood

var (
	configLock    sync.RWMutex
	configuration *config
)

// getConfig returns the current configuration.
func getConfig() *config {
	configLock.RLock()
	defer configLock.RUnlock()
	return configuration
}

var (
	// shutdownChan is closed to indicate that the server is shutting down, and
	// no more connections should be accepted.
	shutdownChan = make(chan struct{})

	activeConnections sync.WaitGroup
)

var configReloadLock sync.Mutex

func reloadConfig() error {
	configReloadLock.Lock()
	defer configReloadLock.Unlock()

	newConf, err := loadConfiguration()
	if err != nil {
		log.Println("Error reloading configuration:", err)
		return err
	}

	configLock.Lock()
	configuration = newConf
	configLock.Unlock()

	accessLog.Open(newConf.AccessLog)
	tlsLog.Open(newConf.TLSLog)
	contentLog.Open(filepath.Join(newConf.ContentLogDir, "index.csv"))
	starlarkLog.Open(newConf.StarlarkLog)
	authLog.Open(newConf.AuthLog)

	customLogLock.Lock()
	for p, l := range customLogs {
		l.Open(p)
	}
	customLogLock.Unlock()

	newConf.openPerUserPorts()

	log.Println("Reloaded configuration")
	return nil
}

func init() {
	hupChan := make(chan os.Signal, 1)
	signal.Notify(hupChan, syscall.SIGHUP)

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGTERM)

	go func() {
		for {
			select {
			case <-termChan:
				log.Println("Received SIGTERM")
				close(shutdownChan)
				conf := getConfig()
				if conf != nil && conf.PIDFile != "" {
					os.Remove(conf.PIDFile)
				}
				go func() {
					// Allow 20 seconds for active connections to finish.
					time.Sleep(20 * time.Second)
					os.Exit(0)
				}()
				// Or exit when all active connections have finished.
				activeConnections.Wait()
				os.Exit(0)

			case <-hupChan:
				log.Println("Received SIGHUP")
				reloadConfig()
			}
		}
	}()
}

func handleReload(w http.ResponseWriter, r *http.Request) {
	err := reloadConfig()
	if err != nil {
		fmt.Fprintln(w, "Error reloading configuration:", err)
		return
	}
	fmt.Fprintln(w, "Reloaded configuration")
}
