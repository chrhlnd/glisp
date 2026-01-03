package glispext

import (
	"io"
	"sync"
	"sync/atomic"
	"log"
)

type ReadOnData func(int, []byte)

type ReadWatcher struct {
	add chan ReadOnData
	rem chan int
	dataIn io.Reader
	watchers map[int]ReadOnData
	dead *atomic.Bool
}

func (rw *ReadWatcher) AddWatcher(fn ReadOnData) {
	if !rw.dead.Load() {
		rw.add <- fn
	}
}

func (rw *ReadWatcher) RemWatcher(id int) {
	if !rw.dead.Load() {
		rw.rem <- id
	}
}

type ReadWatcherCollection struct {
	groups map[int]*ReadWatcher
	groupLock *sync.Mutex
}

func NewReadWatcherCollection() *ReadWatcherCollection {
	return &ReadWatcherCollection{make(map[int]*ReadWatcher), &sync.Mutex{}}
}

func (col *ReadWatcherCollection) RemWatcher(id int, fnId int) {
	col.groupLock.Lock()
	if w, ok := col.groups[id]; ok {
		w.RemWatcher(fnId)
	}
	col.groupLock.Unlock()
}

func (col *ReadWatcherCollection) AddWatcher(id int, in io.Reader, fn ReadOnData) {
	col.NewWatcher(id, in).AddWatcher(fn)
}

func (col *ReadWatcherCollection) NewWatcher(id int, in io.Reader) *ReadWatcher {
	col.groupLock.Lock()
	if ret, ok := col.groups[id]; ok {
		col.groupLock.Unlock()
		return ret
	}
	col.groupLock.Unlock()

	ret := &ReadWatcher{make(chan ReadOnData),
							make(chan int),
							in,
							make(map[int]ReadOnData),
							&atomic.Bool{}}

	col.groupLock.Lock()
	col.groups[id] = ret 
	col.groupLock.Unlock()

	ret.run(col.watcherTerm(id))
	return ret
}

func (col *ReadWatcherCollection) watcherTerm(id int) func() {
	return func() {
		col.groupLock.Lock()
		delete(col.groups, id)
		col.groupLock.Unlock()
	}
}

func (p *ReadWatcher) run(onTerm func()) {
	watcherId := 0
	lock := &sync.Mutex{}

	closeWatchers := func() {
		var empty [0]byte

		callers := make(map[int]ReadOnData)

		lock.Lock()
		for k, v := range p.watchers {
			callers[k] = v
		}
		lock.Unlock()

		for k, v := range callers {
			v(k, empty[:])
		}
	}

	callWatchers := func(data []byte) bool {
		if len(data) == 0 {
			log.Fatal("Watchers called with 0 len data")
		}

		empty := true
		callers := make(map[int]ReadOnData)

		lock.Lock()
		for k, v := range p.watchers {
			callers[k] = v
		}
		lock.Unlock()

		for k, v := range callers {
			v(k, data)
			empty = false
		}

		return !empty
	}

	addWatcher := func(fn ReadOnData) {
		watcherId++
		id := watcherId
		lock.Lock()
		p.watchers[id] = fn
		lock.Unlock()
	}

	remWatcher := func(id int)  {
		lock.Lock()
		delete(p.watchers, id)
		lock.Unlock()
	}

	go func() {
		go func () {
			var data [256]byte
			for {
				n, err := p.dataIn.Read(data[:])
				if err != nil {
					if err != io.EOF {
						log.Println("Watcher had read error %v", err)
					}
					closeWatchers()
					p.dead.Store(true)
					close(p.add)
					close(p.rem)
					break
				}

				// no more watchers, stop watching
				if !callWatchers(data[0:n]) {
					p.dead.Store(true)
					close(p.add)
					close(p.rem)
					break
				}
			}
		}()

		WORK:
		for {
			select {
			case a, ok := <-p.add:
				if !ok {
					break WORK
				}
				addWatcher(a)
			case id, ok := <-p.rem:
				if !ok {
					break WORK
				}
				remWatcher(id)
			}
		}

		onTerm()
	}()
}
