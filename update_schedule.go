package main

import "time"

func updateRetryDelay(failures int) time.Duration {
	delay := time.Minute
	for i := 1; i < failures && delay < 15*time.Minute; i++ {
		delay *= 2
	}
	return min(delay, 15*time.Minute)
}

// true означает ошибку и более раннюю повторную попытку.
func runUpdateSchedule(stop, wake <-chan struct{}, first, regular time.Duration, retry func(int) time.Duration, attempt func() bool) {
	wait, failures := first, 0
	for {
		timer := time.NewTimer(wait)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
		select {
		case <-stop:
			return
		default:
		}
		if attempt() {
			failures++
			wait = retry(failures)
		} else {
			failures = 0
			wait = regular
		}
	}
}
