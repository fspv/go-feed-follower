package feedfollower

import (
	"context"
	"log"
	"sync"
	"time"
)

func RunPeriodically(toRun func(), interval time.Duration, wg *sync.WaitGroup, ctx context.Context) {
	/*
		A wrapper to run function repeatedly over specified intervals of time.

		wg should be provided in order to properfly await function from ouside
		ctx enables graceful shutdown in case interrupt event is received

		During the normal operation the function runs over and over indefinitely and
		wait on wg never returns
	*/

	defer wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	toRun()

	for {
		select {
		case <-ctx.Done():
			log.Println("Requested shutdown of periodic function, exiting")
			return
		case <-ticker.C:
			// TODO: add timeout for this function
			toRun()
		}
	}
}
