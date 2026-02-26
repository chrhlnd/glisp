package glisp

import (
 "time"
 "sync/atomic"
 "sync"
)

//WaitCond condition variable to 
type WaitCond struct {
    trapper chan struct{}
    state int64
    mux *sync.Mutex
}

// Signal flag that this condition has been met
func (c *WaitCond)Signal() {
    if atomic.CompareAndSwapInt64(&c.state, 0, 1) {
        close(c.trapper)
    }
}

// Reset the state of this condition
func (c *WaitCond)Reset() {
    c.mux.Lock()
    
    if c.IsSet() { // this check is to see if we need a new trapper, its been flipped
        c.trapper = make(chan struct{})
        atomic.CompareAndSwapInt64(&c.state, 1, 0)
    }
    
    c.mux.Unlock()
}

// Channel get the channel to wait on 
func (c *WaitCond)Channel() chan struct{} {
    return c.trapper
}

// IsSet Test if the signal is set
func (c *WaitCond)IsSet() bool {
    return atomic.LoadInt64(&c.state) > 0
}

// Wait hold on this condition, delay = 0 waits until the cond is met
func (c *WaitCond)Wait(delay time.Duration) bool {
    if c.IsSet() {
        return true
    }
    
    if delay == 0 {
        <-c.trapper
        return true
    }
    
    select {
        case <-c.trapper:
        case <-time.After(delay):
    }
    
    return c.IsSet()
}

// NewWaitCond make a new condition
func NewWaitCond() *WaitCond {
    return &WaitCond{make(chan struct{}),0,&sync.Mutex{}}
}

