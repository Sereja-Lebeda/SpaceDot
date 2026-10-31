package main

import (
	"testing"
	"time"
)

func TestUpdateRetryDelay(t *testing.T) {
	for i, minutes := range []int{1, 2, 4, 8, 15, 15, 15} {
		if got := updateRetryDelay(i + 1); got != time.Duration(minutes)*time.Minute {
			t.Fatalf("failure %d: %v", i+1, got)
		}
	}
}

func TestUpdateScheduleWakeAndRetry(t *testing.T) {
	stop, wake := make(chan struct{}), make(chan struct{}, 1)
	calls, done := make(chan int, 4), make(chan struct{})
	go func() {
		defer close(done)
		count := 0
		runUpdateSchedule(stop, wake, time.Hour, time.Hour, func(int) time.Duration { return time.Millisecond }, func() bool {
			count++
			calls <- count
			return count == 1
		})
	}()
	wake <- struct{}{}
	for want := 1; want <= 2; want++ {
		select {
		case got := <-calls:
			if got != want {
				t.Fatal(got)
			}
		case <-time.After(time.Second):
			t.Fatal("wake or retry did not run")
		}
	}
	// После успешной проверки следующая попытка снова следует обычному расписанию.
	select {
	case got := <-calls:
		t.Fatal("retry did not reset", got)
	default:
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
