package test

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

//一写多读
func Test_Chan_OtoM(t *testing.T) {
	wg := &sync.WaitGroup{}
	ch := make(chan int, 100)
	send := func() {
		for i := 0; i < 100; i++ {
			ch <- i
		}
		// signal sending finish
		close(ch)
	}
	recv := func(id int) {
		defer wg.Done()
		for i := range ch {
			fmt.Printf("receiver #%d get %d\n", id, i)
		}
		fmt.Printf("receiver #%d exit\n", id)
	}
	wg.Add(3)
	go recv(0)
	go recv(1)
	go recv(2)
	send()
	wg.Wait()
}

//多写一读
func Test_Chan_MtoO(t *testing.T) {
	wg := &sync.WaitGroup{}
	ch := make(chan int, 100)
	done := make(chan struct{})
	send := func(id int) {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				// get exit signal
				fmt.Printf("sender #%d exit\n", id)
				return
			case ch <- id*1000 + i:
			}
		}
	}
	recv := func() {
		count := 0
		for i := range ch {
			fmt.Printf("receiver get %d\n", i)
			count++
			if count >= 1000 {
				// signal recving finish
				close(done)
				return
			}
		}
	}
	wg.Add(3)
	go send(0)
	go send(1)
	go send(2)
	recv()
	wg.Wait()
}

//多写多读
func Test_Chan_MtoM(t *testing.T) {
	wg := &sync.WaitGroup{}
	ch := make(chan int, 100)
	done := make(chan struct{})
	send := func(id int) {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				// get exit signal
				fmt.Printf("sender #%d exit\n", id)
				return
			case ch <- id*1000 + i:
			}
		}
	}
	recv := func(id int) {
		defer wg.Done()
		for {
			select {
			case <-done:
				// get exit signal
				fmt.Printf("receiver #%d exit\n", id)
				return
			case i := <-ch:
				fmt.Printf("receiver #%d get %d\n", id, i)
				time.Sleep(time.Millisecond)
			}
		}
	}
	wg.Add(6)
	go send(0)
	go send(1)
	go send(2)
	go recv(0)
	go recv(1)
	go recv(2)
	time.Sleep(time.Second)
	// signal finish
	close(done)
	// wait all sender and receiver exit
	wg.Wait()
}
