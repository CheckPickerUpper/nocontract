package running_test

import (
	"strings"
	"sync"
)

// shopExitStderrLines is how many of its last standard error lines a shop's exit keeps.
const shopExitStderrLines = 20

// shopListeningNotice is what a shop's standard output held after a point a test marked: its
// next "Listening on" notice, or its exit.
//
//sumtype:decl
type shopListeningNotice interface {
	isShopListeningNotice()
}

// shopListening is a "Listening on" notice and the address it named.
type shopListening struct {
	address string
}

// shopExited is the end of a shop's standard output and standard error, with the last lines it
// wrote to standard error.
type shopExited struct {
	stderr []string
}

func (shopListening) isShopListeningNotice() {}
func (shopExited) isShopListeningNotice()    {}

// shopNoticeMark is how many notices a shop had printed when a test marked it.
type shopNoticeMark struct {
	count int
}

// shopListeningNotices keeps, in order, every notice a shop printed after its first "Listening
// on" line. Its exit, when it comes, is the last notice.
type shopListeningNotices struct {
	mutex    *sync.Mutex
	added    *sync.Cond
	received []shopListeningNotice
}

func newShopListeningNotices() *shopListeningNotices {
	mutex := &sync.Mutex{}
	return &shopListeningNotices{mutex: mutex, added: sync.NewCond(mutex), received: []shopListeningNotice{}}
}

// add records the shop's next notice and wakes every waiter.
func (notices *shopListeningNotices) add(notice shopListeningNotice) {
	notices.mutex.Lock()
	defer notices.mutex.Unlock()
	notices.received = append(notices.received, notice)
	notices.added.Broadcast()
}

// mark is taken before a test makes the shop print a notice, so the wait for that notice cannot
// take one printed earlier.
func (notices *shopListeningNotices) mark() shopNoticeMark {
	notices.mutex.Lock()
	defer notices.mutex.Unlock()
	return shopNoticeMark{count: len(notices.received)}
}

// after delivers the first notice the shop printed after mark, or its exit when the shop exited
// before printing one.
func (notices *shopListeningNotices) after(mark shopNoticeMark) <-chan shopListeningNotice {
	arrived := make(chan shopListeningNotice, 1)
	go func() {
		notices.mutex.Lock()
		defer notices.mutex.Unlock()
		for {
			if len(notices.received) > mark.count {
				arrived <- notices.received[mark.count]
				return
			}
			if len(notices.received) > 0 {
				if exited, isExited := notices.received[len(notices.received)-1].(shopExited); isExited {
					arrived <- exited
					return
				}
			}
			notices.added.Wait()
		}
	}()
	return arrived
}

// lastLines is at most count of the last lines of text.
func lastLines(text string, count int) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return lines[max(0, len(lines)-count):]
}
