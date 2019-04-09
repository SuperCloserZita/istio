// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package appsignals

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"istio.io/istio/pkg/log"

	"github.com/howeyc/fsnotify"
)

var handlers struct {
	sync.Mutex
	listeners []chan<- Signal
	signals   chan os.Signal
}

type Signal struct {
	// Source of the event trigger as we simulate signal generation from a variety of triggers
	Source string
	Signal os.Signal
}

// Notify a channel if a an event is triggered. A notification is always triggered for SIGUSR1
func Watch(c chan<- Signal) {
	if c == nil {
		panic("reload: Watch using nil channel")
	}

	handlers.Lock()
	defer handlers.Unlock()

	if handlers.listeners == nil {
		// Watch for SIGUSR1 by default
		handlers.signals = make(chan os.Signal, 1)
		signal.Notify(handlers.signals, syscall.SIGUSR1)
		go func() {
			for range handlers.signals {
				Notify("os", syscall.SIGUSR1)
			}
		}()
		handlers.listeners = make([]chan<- Signal, 0, 10)
	}
	handlers.listeners = append(handlers.listeners, c)
}

// Directly trigger a notification
func Notify(trigger string, signal os.Signal) {
	handlers.Lock()
	defer handlers.Unlock()
	for _, v := range handlers.listeners {
		select {
		case v <- Signal{trigger, signal}:
		default:
		}
	}
}

// Trigger notifications when a file is mutated
func FileTrigger(path string, signal os.Signal, shutdown chan os.Signal) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	err = watcher.Watch(path)
	if err != nil {
		return err
	}
	go func() {
		loop := true
		for loop {
			select {
			case _, ok := <-watcher.Event:
				if ok {
					log.Warnf("File watch triggered: %v", path)
					Notify(path, signal)
				} else {
					loop = false
				}
			case err := <-watcher.Error:
				log.Warnf("Error watching file trigger: %v %v", path, err)
				loop = false
			case signal := <-shutdown:
				log.Infof("Shutting down file watcher: %v %v", path, signal)
				loop = false
			}
		}
		err = watcher.Close()
		if err != nil {
			log.Warnf("Error stopping file watcher: %v %v", path, err)
		}
	}()
	return nil
}