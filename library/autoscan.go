package library

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchDebounce is how long the watcher waits after the last filesystem event
// of a library before rescanning, so a burst of downloads collapses into one scan.
const watchDebounce = 20 * time.Second

// AutoScanner triggers rescans from two sources:
//   - a periodic timer (settings key auto_scan_hours, 0 = off);
//   - an optional fsnotify watcher over root / source / series folders.
type AutoScanner struct {
	store   *Store
	scanner *Scanner
	watch   bool

	mu       sync.Mutex
	interval time.Duration
	resetCh  chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once

	watcher   *fsnotify.Watcher
	debTimers map[string]*time.Timer
}

// NewAutoScanner creates an auto-scanner. Set watch=false to disable fsnotify.
func NewAutoScanner(store *Store, scanner *Scanner, watch bool) *AutoScanner {
	return &AutoScanner{
		store:     store,
		scanner:   scanner,
		watch:     watch,
		resetCh:   make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		debTimers: map[string]*time.Timer{},
	}
}

// Interval returns the periodic rescan interval (0 = disabled).
func (a *AutoScanner) Interval() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.interval
}

// SetIntervalHours persists the interval and re-arms the timer live.
func (a *AutoScanner) SetIntervalHours(hours int) error {
	if hours < 0 {
		hours = 0
	}
	if err := a.store.SetSetting(SettingAutoScanHours, strconv.Itoa(hours)); err != nil {
		return err
	}
	a.mu.Lock()
	a.interval = time.Duration(hours) * time.Hour
	a.mu.Unlock()
	select {
	case a.resetCh <- struct{}{}:
	default:
	}
	return nil
}

// Start loads the persisted interval and launches the timer and watcher loops.
func (a *AutoScanner) Start() error {
	hours := 0
	if v, err := a.store.GetSetting(SettingAutoScanHours, "0"); err == nil {
		hours, _ = strconv.Atoi(strings.TrimSpace(v))
	}
	a.mu.Lock()
	a.interval = time.Duration(hours) * time.Hour
	a.mu.Unlock()

	go a.tickLoop()

	if a.watch {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			log.Printf("autoscan: watcher disabled: %v", err)
		} else {
			a.watcher = w
			go a.watchLoop()
			a.refreshWatches()
		}
	}
	log.Printf("autoscan: started (periodic=%s, watcher=%v)", intervalLabel(a.Interval()), a.watcher != nil)
	return nil
}

// Stop tears down the loops.
func (a *AutoScanner) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		if a.watcher != nil {
			_ = a.watcher.Close()
		}
		a.mu.Lock()
		for _, t := range a.debTimers {
			t.Stop()
		}
		a.mu.Unlock()
	})
}

func intervalLabel(d time.Duration) string {
	if d <= 0 {
		return "off"
	}
	return d.String()
}

func (a *AutoScanner) tickLoop() {
	for {
		d := a.Interval()
		var c <-chan time.Time
		var timer *time.Timer
		if d > 0 {
			timer = time.NewTimer(d)
			c = timer.C
		}
		select {
		case <-a.stopCh:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-a.resetCh:
			if timer != nil {
				timer.Stop()
			}
		case <-c:
			log.Printf("autoscan: periodic rescan of all libraries")
			a.ScanAll("scheduled")
		}
	}
}

// ScanAll scans every library one after the other (blocking).
func (a *AutoScanner) ScanAll(reason string) {
	libs, err := a.store.ListLibraries()
	if err != nil {
		log.Printf("autoscan: list libraries: %v", err)
		return
	}
	for _, lib := range libs {
		if err := a.scanner.Scan(lib, reason); err != nil && !errors.Is(err, ErrScanRunning) {
			log.Printf("autoscan: scan %s: %v", lib.Name, err)
		}
	}
	a.refreshWatches()
}

func (a *AutoScanner) watchLoop() {
	for {
		select {
		case <-a.stopCh:
			return
		case ev, ok := <-a.watcher.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod {
				continue
			}
			if lib := a.libraryForPath(ev.Name); lib != nil {
				a.scheduleScan(lib.ID)
			}
		case err, ok := <-a.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("autoscan: watcher error: %v", err)
		}
	}
}

// scheduleScan (re)arms the per-library debounce timer.
func (a *AutoScanner) scheduleScan(libID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t, ok := a.debTimers[libID]; ok {
		t.Stop()
	}
	a.debTimers[libID] = time.AfterFunc(watchDebounce, func() {
		fresh, err := a.store.GetLibrary(libID)
		if err != nil {
			return
		}
		err = a.scanner.Scan(fresh, "filesystem change")
		if errors.Is(err, ErrScanRunning) {
			a.scheduleScan(libID) // try again once the running scan ends
			return
		}
		if err != nil {
			log.Printf("autoscan: scan %s: %v", fresh.Name, err)
		}
		a.refreshWatches()
	})
}

func (a *AutoScanner) libraryForPath(p string) *Library {
	libs, err := a.store.ListLibraries()
	if err != nil {
		return nil
	}
	for _, lib := range libs {
		if underRoot(lib.RootPath, p) {
			return lib
		}
	}
	return nil
}

// refreshWatches watches each library root, its mangas folder, source folders
// and series folders (fsnotify is not recursive; chapter folders are skipped).
func (a *AutoScanner) refreshWatches() {
	if a.watcher == nil {
		return
	}
	libs, err := a.store.ListLibraries()
	if err != nil {
		return
	}
	for _, lib := range libs {
		root := lib.RootPath
		_ = a.watcher.Add(root)
		mroot := detectMangasRoot(root)
		if filepath.Clean(mroot) != filepath.Clean(root) {
			_ = a.watcher.Add(mroot)
		}
		des, err := os.ReadDir(mroot)
		if err != nil {
			continue
		}
		for _, de := range des {
			if !isDirEntry(de, mroot) || skipName(de.Name()) {
				continue
			}
			sp := filepath.Join(mroot, de.Name())
			_ = a.watcher.Add(sp)
			sdes, err := os.ReadDir(sp)
			if err != nil {
				continue
			}
			for _, sd := range sdes {
				if isDirEntry(sd, sp) && !skipName(sd.Name()) {
					_ = a.watcher.Add(filepath.Join(sp, sd.Name()))
				}
			}
		}
	}
}
