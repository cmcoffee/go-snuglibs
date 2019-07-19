package nfo

import (
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"fmt"
)

var (
	// Signal Notification Channel. (ie..nfo.Signal<-os.Kill will initiate a shutdown.)
	signalChan  = make(chan os.Signal)
	globalDefer []func() error
	errCode     = 0
	wait        sync.WaitGroup
	exit_lock   = make(chan struct{})
)

// Global wait group, allows running processes to finish up tasks before app shutdown
func BlockShutdown() {
	wait.Add(1)
}

// Task completed, carry on with shutdown.
func UnblockShutdown() {
	wait.Done()
}

// Adds a function to the global defer, function must take no arguments and either return nothing or return an error.
func Defer(closer interface{}) {
	errorWrapper := func(closerFunc func()) func() error {
		return func() error {
			closerFunc()
			return nil
		}
	}

	switch closer := closer.(type) {
	case func():
		globalDefer = append(globalDefer, errorWrapper(closer))
	case func() error:
		globalDefer = append(globalDefer, closer)
	}
}

// Intended to be a defer statement at the begining of main, but can be called at anytime with an exit code.
// Tries to catch a panic if possible and log it as a fatal error,
// then proceeds to send a signal to the global defer/shutdown handler
func Exit(exit_code int) {
	if r := recover(); r != nil {
		_, f, line, _ := runtime.Caller(4)
		_, file := filepath.Split(f)
		Debug(string(debug.Stack())) // Output full stacktrace to Debug logger.
		Fatal("(panic) %s, line %d: %s", file, line, r)
	} else {
		atomic.StoreInt32(&fatal_triggered, 1) // Ignore any Fatal() calls, we've been told to exit.
		signalChan <- os.Kill
		<-exit_lock
		os.Exit(exit_code)
	}
}

// Sets the signals that we listen for.
func SetSignals(sig ...os.Signal) {
	mutex.Lock()
	defer mutex.Unlock()
	signal.Stop(signalChan)
	signal.Notify(signalChan, sig...)
}

// Set a callback function(no arguments) to run after receiving a specific syscall, function returns true to continue shutdown process.
func SignalCallback(signal os.Signal, callback func() (continue_shutdown bool)) {
	mutex.Lock()
	defer mutex.Unlock()
	callbacks[signal] = callback
}

var callbacks = make(map[os.Signal]func() bool)

func init() {
	SetSignals(syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		var err error
		for {
			s := <-signalChan

			write2log(_flash_txt, fmt.Sprintf("%s  ", string(flush_line)))

			mutex.Lock()
			cb := callbacks[s]
			mutex.Unlock()

			if cb != nil {
				if !cb() {
					continue
				}
			}

			switch s {
			case syscall.SIGINT:
				errCode = 130
			case syscall.SIGHUP:
				errCode = 129
			case syscall.SIGTERM:
				errCode = 143
			}

			break
		}
		// Run through all globalDefer functions.
		for _, x := range globalDefer {
			if err = x(); err != nil {
				write2log(ERROR, err.Error())
			}
		}

		// Wait on any process that have access to wait.
		wait.Wait()

		// Close out all open files.
		for name := range open_files {
			Close(name)
		}

		// Blank out last line.
		write2log(_flash_txt)

		// Finally exit the application
		select {
		case exit_lock <- struct{}{}:
		default:
			os.Exit(errCode)
		}
	}()
}
