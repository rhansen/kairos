package kairos

import (
	"sync"
	"time"
)

var (
	mutex       sync.Mutex
	timers      []*Timer
	rescheduleC = make(chan struct{}, 1)
)

func init() {
	go timerRoutine()
}

// Add the timer to the heap.
func addTimer(t *Timer, d time.Duration) {
	t.when = time.Now().Add(d)

	mutex.Lock()
	addTimerLocked(t)
	mutex.Unlock()
}

func addTimerLocked(t *Timer) {
	t.i = len(timers)
	timers = append(timers, t)
	siftupTimer(t.i)

	// Reschedule if this is the next timer in the heap.
	if t.i == 0 {
		reschedule()
	}
}

// Delete timer t from the heap.
// It returns true if t was removed, false if t wasn't even there.
// Do not need to update the timer routine: if it wakes up early, no big deal.
func delTimer(t *Timer) (b bool) {
	mutex.Lock()
	b = delTimerLocked(t)
	mutex.Unlock()
	return
}

// Delete timer t from the heap.
// It returns true if t was removed, false if t wasn't even there.
// Do not need to update the timer routine: if it wakes up early, no big deal.
func delTimerLocked(t *Timer) bool {
	// t may not be registered anymore and may have
	// a bogus i (typically 0, if generated by Go).
	// Verify it before proceeding.
	i := t.i
	last := len(timers) - 1
	if i < 0 || i > last || timers[i] != t {
		return false
	}
	if i != last {
		timers[i] = timers[last]
		timers[i].i = i
	}
	timers[last] = nil
	timers = timers[:last]
	if i != last {
		siftupTimer(i)
		siftdownTimer(i)
	}
	return true
}

// Reset the timer to the new timeout duration.
// This clears the channel.
func resetTimer(t *Timer, d time.Duration) (b bool) {
	mutex.Lock()
	b = delTimerLocked(t)
	// The channel must be drained while the mutex is locked, otherwise a notification generated by a
	// concurrent t.Reset(0) call might be erroneously consumed.
	select {
	case <-t.C:
	default:
	}
	t.when = time.Now().Add(d)
	addTimerLocked(t)
	mutex.Unlock()
	return
}

func reschedule() {
	// Do not block if there is already a pending reschedule request.
	select {
	case rescheduleC <- struct{}{}:
	default:
	}
}

func timerRoutine() {
	var now time.Time
	var last int

	sleepTimer := time.NewTimer(0)
	<-sleepTimer.C
	sleepTimerActive := false

Loop:
	for {
		select {
		case <-sleepTimer.C:

		case <-rescheduleC:
			// If not yet received a value from sleepTimer.C, the timer must be
			// stopped and—if Stop reports that the timer expired before being
			// stopped—the channel explicitly drained.
			if !sleepTimer.Stop() && sleepTimerActive {
				<-sleepTimer.C
			}
		}
		sleepTimerActive = false

	Reschedule:
		now = time.Now()

		mutex.Lock()
		if len(timers) == 0 {
			mutex.Unlock()
			continue Loop
		}

		t := timers[0]
		delta := t.when.Sub(now)

		// Sleep if not expired.
		if delta > 0 {
			mutex.Unlock()
			sleepTimer.Reset(delta)
			sleepTimerActive = true
			continue Loop
		}

		// Timer expired. Trigger the timer's function callback.
		t.f(&now)

		// Remove from heap.
		last = len(timers) - 1
		if last > 0 {
			timers[0] = timers[last]
			timers[0].i = 0
		}
		timers[last] = nil
		timers = timers[:last]
		if last > 0 {
			siftdownTimer(0)
		}
		t.i = -1 // mark as removed

		mutex.Unlock()

		// Reschedule immediately.
		goto Reschedule
	}
}

// Heap maintenance algorithms.
// Based on golang source /runtime/time.go

func siftupTimer(i int) {
	tmp := timers[i]
	when := tmp.when

	var p int
	for i > 0 {
		p = (i - 1) / 4 // parent
		if !when.Before(timers[p].when) {
			break
		}
		timers[i] = timers[p]
		timers[i].i = i
		timers[p] = tmp
		timers[p].i = p
		i = p
	}
}

func siftdownTimer(i int) {
	n := len(timers)
	when := timers[i].when
	tmp := timers[i]
	for {
		c := i*4 + 1 // left child
		c3 := c + 2  // mid child
		if c >= n {
			break
		}
		w := timers[c].when
		if c+1 < n && timers[c+1].when.Before(w) {
			w = timers[c+1].when
			c++
		}
		if c3 < n {
			w3 := timers[c3].when
			if c3+1 < n && timers[c3+1].when.Before(w3) {
				w3 = timers[c3+1].when
				c3++
			}
			if w3.Before(w) {
				w = w3
				c = c3
			}
		}
		if !w.Before(when) {
			break
		}
		timers[i] = timers[c]
		timers[i].i = i
		timers[c] = tmp
		timers[c].i = c
		i = c
	}
}
