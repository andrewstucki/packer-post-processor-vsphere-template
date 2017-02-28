package main

import (
	"context"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/progress"
	"github.com/vmware/govmomi/vim25/types"
)

type uploadItem struct {
	url  *url.URL
	item types.OvfFileItem
	ch   chan progress.Report
}

func (u uploadItem) Sink() chan<- progress.Report {
	return u.ch
}

type UploaderProgress struct {
	client *vim25.Client
	lease  *object.HttpNfcLease

	pos   int64 // Number of bytes
	total int64 // Total number of bytes

	done chan struct{} // When lease updater should stop

	wg sync.WaitGroup // Track when update loop is done
}

func NewUploaderProgress(client *vim25.Client, lease *object.HttpNfcLease, items []uploadItem) *UploaderProgress {
	progress := UploaderProgress{
		client: client,
		lease:  lease,

		done: make(chan struct{}),
	}

	for _, item := range items {
		progress.total += item.item.Size
		go progress.waitForProgress(item)
	}

	// Kickstart update loop
	progress.wg.Add(1)
	go progress.run()

	return &progress
}

func (p *UploaderProgress) waitForProgress(item uploadItem) {
	var pos, total int64

	total = item.item.Size

	for {
		select {
		case <-p.done:
			return
		case progress, ok := <-item.ch:
			// Return in case of error
			if ok && progress.Error() != nil {
				return
			}

			if !ok {
				// Last element on the channel, add to total
				atomic.AddInt64(&p.pos, total-pos)
				return
			}

			// Approximate progress in number of bytes
			x := int64(float32(total) * (progress.Percentage() / 100.0))
			atomic.AddInt64(&p.pos, x-pos)
			pos = x
		}
	}
}

func (p *UploaderProgress) run() {
	defer p.wg.Done()

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-tick.C:
			// From the vim api HttpNfcLeaseProgress(percent) doc, percent ==
			// "Completion status represented as an integer in the 0-100 range."
			// Always report the current value of percent, as it will renew the
			// lease even if the value hasn't changed or is 0.
			percent := int32(float32(100*atomic.LoadInt64(&p.pos)) / float32(p.total))
			err := p.lease.HttpNfcLeaseProgress(context.TODO(), percent)
			if err != nil {
				log.Printf("from lease updater: %s\n", err)
			}
		}
	}
}

func (p *UploaderProgress) Done() {
	close(p.done)
	p.wg.Wait()
}
