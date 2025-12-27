package syncdir

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Debouncer struct {
	mu       sync.Mutex
	timers   map[string]*time.Timer
	interval time.Duration
}

func StartSync() {
	fileQueue := make(chan string, 100)
	debouncer := NewDebouncer(500 * time.Millisecond)

	go relayWorker(fileQueue)

	startWatching("./sync-folder", fileQueue, debouncer)

	select {}
}

func relayWorker(queue <-chan string) {
	fmt.Println("Relay worker started. Waiting for files...")
	go func() {
		for path := range queue {
			fmt.Printf("[Relay] Processing file: %s\n", path)
			// Logic for chunking and sending would go here
			// sendToRelay(path)
		}
	}()
}

func startWatching(rootPath string, queue chan<- string, debouncer *Debouncer) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					debouncer.Run(event.Name, func() {
						info, err := os.Stat(event.Name)
						if err == nil && !info.IsDir() {
							// push the file path into the channel
							queue <- event.Name
						}
					})
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Watcher error:", err)
			}
		}
	}()

	// watch root and subdirectories
	filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return watcher.Add(path)
		}
		return nil
	})
}

func NewDebouncer(interval time.Duration) *Debouncer {
	fmt.Printf("Monitoring with %dms debounce...\n", interval.Milliseconds())
	return &Debouncer{
		timers:   make(map[string]*time.Timer),
		interval: interval,
	}
}

func (d *Debouncer) Run(key string, action func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if timer, ok := d.timers[key]; ok {
		timer.Stop()
	}

	d.timers[key] = time.AfterFunc(d.interval, func() {
		action()
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
	})
}
