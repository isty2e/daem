package raceprobe

import (
	"sync"
	"testing"
)

func TestIntentionalRaceDetectorProof(t *testing.T) {
	var value int
	start := make(chan struct{})
	var finished sync.WaitGroup
	finished.Add(2)
	for range 2 {
		go func() {
			defer finished.Done()
			<-start
			value++
		}()
	}
	close(start)
	finished.Wait()
}
